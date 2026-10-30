# PRD: Tessera

**Status:** design phase. Nothing described here has been built or
measured. Targets are design inputs to validate by benchmark.

## Problem

Every marketplace that moves people or goods has to answer two
questions in real time: who gets this request, and what does it cost.
Both are easy to answer badly. A demo that sorts a table by distance
will hand the same driver to two riders under concurrent load. A
pricing system built on one "surge multiplier" number cannot explain
itself when a rider disputes a fare, and cannot be tested beyond
"the number looked right."

Tessera is a from-scratch answer to a narrower question than "build
Uber": what do the matching and pricing cores look like when the
guarantees are enforced by the system and not by every caller
remembering to be careful? Specifically:

- a driver can never hold two active assignments, even if the fast
  layer is wrong
- a price can always be reproduced from stored inputs
- every state change is explicit, legal, and recorded
- when the ephemeral layer (Redis) disagrees with the durable one
  (Postgres), the disagreement is detected and recorded, not silently
  tolerated

This project also composes skills built earlier in the portfolio
(atomic check-and-act scripts, database-level backstops, append-only
histories, cache-versus-truth reconciliation, WebSocket delivery) into
one system under real-time and geographic constraints.

## Users

This is a portfolio and reference project, not a product with real
users. The intended audiences:

- **Me**, as the concrete artifact for backend and system design
  interviews where dispatch, matching, and correctness under
  concurrency are the topic.
- **A reviewer** (interviewer, hiring manager, peer engineer) judging
  whether the claims hold up: the double-assignment defense, the
  pricing explainability, the failure handling, the honesty about gaps.

For the purpose of the system's own design, the actors are:

- **Rider:** requests a quote, books a ride, watches the driver
  approach, can cancel before pickup.
- **Driver:** goes online and offline, streams location, receives and
  answers offers, advances a trip through arrival, start, completion.
- **Operator (admin):** manages pricing rule sets, inspects surge cells
  and reconciliation history.

## Scope

**In scope** (mechanism for each is in `docs/ARCHITECTURE.md`):

1. Geospatial candidate search: Redis GEO for live availability,
   expanding radius rings, ETA-based ranking behind a replaceable
   interface.
2. Atomic driver claim: Lua script removal from the available set,
   backed by Postgres partial unique indexes.
3. Sequential offers with timeouts, decline handling, a search budget,
   and deadlines that survive process crashes.
4. A dispatch state machine enforced in Go, by compare-and-swap
   writes, and by a database trigger, with an append-only event log.
5. A pricing rules engine: ordered, declarative, versioned rules
   evaluated by a pure function, integer money, basis-point
   multipliers, explainable quotes.
6. A scheduled surge calculator with a thin-market guard, zero-supply
   handling, and a fail-safe fallback to 1.0x.
7. Quote locking: the price shown at request time is the price charged.
8. Idempotent ride creation.
9. Real-time WebSockets for drivers (offers, trip updates) and riders
   (driver location, trip updates), with bounded buffers,
   latest-wins location frames, heartbeats, rate limits, and
   plausibility checks.
10. Trip breadcrumbs stored in PostGIS for dispute resolution, with
    configurable retention.
11. A scheduled reconciler that checks Redis state against Postgres and
    records every run.
12. Metrics and structured logs sufficient to see the system's health.
13. A driver and rider simulator for demos and load tests.
14. Placeholder JWT auth with role boundaries (rider, driver, admin).

**Explicitly out of scope:**

- **Real road routing and live-traffic ETA.** The `ETAProvider`
  interface exists; the v1 implementation is haversine with a
  circuity factor. A routing engine is a separate project.
- **Payments, payouts, and settlement.** Tessera produces a fare. Moving
  money is a different problem, already explored in LedgerLine.
- **Rider and driver mobile apps, map rendering.** A Go simulator stands
  in for both.
- **Machine-learned pricing or demand forecasting.** The point of this
  project is explicit, testable rules.
- **Pooled rides, scheduled rides, multi-stop trips.** Each is its own
  matching problem.
- **Multi-region deployment.** Keys are region-scoped so it is
  possible later; only one region is implemented.
- **Production identity, device attestation, and serious anti-spoofing.**
  A speed plausibility check is the only guard.
- **Dispute handling for cancellations during a ride.** That transition
  is intentionally not in the state machine.
- **Final-fare reconciliation against metered distance.** v1 charges the
  locked upfront quote. Documented as an extension.

## Functional requirements

| ID | Requirement |
|---|---|
| FR-1 | A rider can request a quote for a pickup and dropoff and receive a price, a quote id, the applied pricing rules, and an expiry. |
| FR-2 | A rider can create a ride only with a valid, unexpired quote id. The trip is created at the quoted price. |
| FR-3 | Repeating a ride creation with the same Idempotency-Key returns the original trip and creates no second one. |
| FR-4 | A rider can have at most one active trip at a time. |
| FR-5 | A driver can go online and offline, and stream location while online. |
| FR-6 | Only drivers currently available, fresh, and nearby are considered for a trip. |
| FR-7 | A candidate driver is offered the trip for a bounded time. Accept, decline, and expiry each resolve exactly once. |
| FR-8 | On decline or expiry, the next best candidate is offered the trip, widening the search until a budget is exhausted, after which the trip becomes NO_DRIVERS. |
| FR-9 | A driver cannot hold two pending offers or two active trips. |
| FR-10 | Trip status moves only along the transitions in the state machine table. Every transition is recorded with actor and timestamp. |
| FR-11 | A rider connected over WebSocket receives the assigned driver's location and trip status updates for their trip only. |
| FR-12 | A driver connected over WebSocket receives offers and trip updates addressed to them only. |
| FR-13 | An operator can publish a new pricing rule set version. Existing quotes keep the version they were computed with. |
| FR-14 | An operator can view surge multipliers by cell, with the inputs that produced them. |
| FR-15 | An operator can view reconciliation run history and findings. |

## Non-functional requirements

All targets below are to be **validated by benchmark** before being
stated as results anywhere.

| ID | Requirement | Target |
|---|---|---|
| NFR-1 | Candidate retrieval latency at 50,000 available drivers | p99 under 10 ms |
| NFR-2 | Request-to-first-offer latency, excluding network | p95 under 500 ms |
| NFR-3 | Location ingest | sustain about 12,500 pings/s (50,000 drivers at 1 ping per 4 s) |
| NFR-4 | Concurrent WebSockets | 50,000 open connections across gateway replicas |
| NFR-5 | Double assignments under contention | zero across 1,000 concurrent claims on one driver |
| NFR-6 | Recovery from Redis loss | available set repopulates within one ping interval; pending offers recovered from Postgres |
| NFR-7 | Location data retention | configurable, default 30 days, enforced by a scheduled purge |
| NFR-8 | Reproducibility of any quote | identical stored inputs and rule set version yield identical output |

## Success criteria

The project is "done" when each of these is demonstrated, not asserted:

1. **Contention test:** 1,000 concurrent attempts to claim one driver
   produce exactly one winner and zero double assignments.
2. **Backstop test:** bypassing Redis and the application, a raw SQL
   attempt to create a second active assignment for one driver fails.
3. **Race test:** an offer accept and its expiry arriving together
   resolve to exactly one outcome, over many repeated runs.
4. **State machine test:** every (state, event) pair is tested; only the
   listed transitions succeed, and a raw SQL illegal transition is
   rejected by the trigger.
5. **Immutability test:** UPDATE and DELETE on `trip_events` are
   rejected by the database.
6. **Pricing test:** golden-file cases pass, and property tests confirm
   the fare never falls below the minimum, the multiplier never exceeds
   the cap, and the function is deterministic.
7. **Quote integrity:** an expired quote is rejected, and no search
   ever reprices a locked quote.
8. **Recovery test:** flushing Redis mid-search does not lose a pending
   offer or leave a driver permanently stuck.
9. **Reconciliation test:** each class of injected drift is detected
   and recorded.
10. **Backpressure test:** a deliberately slow WebSocket consumer does
    not stall other connections.
11. **Benchmarks:** NFR-1 through NFR-5 measured on stated hardware,
    with the results (and the hardware) written into `TESTING.md`.

## Constraints and assumptions

- Single region, single Redis primary, single Postgres primary.
- One Redis key family per region, so a second region is a second key
  set, not a redesign.
- Haversine distance times a circuity factor (default 1.3) divided by
  an assumed speed approximates ETA. This is knowingly wrong near
  rivers, highways, and one-way grids.
- Clients send timestamps and sequence numbers; the server trusts
  neither blindly and applies a plausibility check.
- The load figures in the architecture document are assumptions chosen
  to be large enough that naive designs fail visibly.

## Risks and open questions

| Risk or question | Mitigation or default |
|---|---|
| Dual write between trip creation and stream publish can strand a trip | Reconciler re-enqueues stale SEARCHING trips; outbox listed as an upgrade |
| Redis single point of failure | Recoverable by design; Postgres is the source of truth; HA Redis is deployment, not design |
| Surge oscillation | Smoothing rule limits per-interval movement |
| Surge spike from tiny samples | Thin-market guard ignores ratios below a minimum demand sample |
| Stale calculator causes stuck high prices | TTL on surge keys and 1.0x fallback with an alert |
| Greedy matching is globally suboptimal | `Strategy` interface so batched matching can replace it |
| WebSocket library choice | Open decision, default to `coder/websocket` |
| Geohash cell edge effects for surge | Accepted for v1; H3 listed as an improvement |

## Milestones

Aligned with the build phases in `docs/CURSOR_CONTEXT.md`.

1. Schema, triggers, indexes
2. Geo index and atomic claim
3. State machine
4. Matcher and offers
5. Pricing engine
6. Surge calculator
7. REST API and auth
8. WebSocket gateway
9. Reconciler
10. Simulator, benchmarks, and test report

Priority if time is short: phases 1 to 4 prove the core claim, phase 5
is the second pillar, phases 7 and 10 make it demonstrable.