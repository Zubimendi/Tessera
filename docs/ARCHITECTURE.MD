# Architecture: Tessera

Same format as the rest of this portfolio: each section names a principle,
the problem it answers for a dispatch system specifically, the concrete
mechanism, and the trade-off accepted. Numbers marked "assumption" or
"target" are design inputs to be validated by benchmark, not measured
results. Nothing here has been built yet.

## System shape

```
 rider app                                         driver app
    |  REST                                           |  REST + WebSocket
    v                                                 v
+---------------------------------------------------------------+
|  api (stateless, N replicas)                                  |
|  REST handlers | WS gateway (driver + rider sockets)          |
+---+------------------+------------------+---------------------+
    |                  |                  |
    | trips, offers    | location pings   | quote
    v                  v                  v
+----------+   +------------------+   +------------------+
| Postgres |   | Redis            |   | pricing engine   |
| PostGIS  |   |  geo:avail:{r}   |   | (pure functions) |
|          |   |  driver:{id}     |   +--------+---------+
| trips    |   |  offers:deadline |            ^
| trip_    |   |  surge:{cell}    |            | reads surge
|  events  |   |  stream:dispatch |   +--------+---------+
| offers   |   |  pub/sub trip:*  |   | surge calculator |
| zones    |   +---------+--------+   | (scheduled)      |
| rules    |             ^            +------------------+
+----+-----+             |
     ^                   | XREADGROUP
     |   CAS state   +---+--------------+       +------------------+
     +---------------+ matcher workers  |       | reconciler       |
                     | (offer loop)     |       | (scheduled)      |
                     +------------------+       +------------------+
```

Postgres is the system of record for every business fact (trips, offers,
prices charged, the event history). Redis holds only what is ephemeral or
derivable: live driver positions, claim state, deadlines, surge numbers.
If Redis is wiped, drivers reappear within one ping interval and in-flight
offers are rebuilt from Postgres. If Postgres and Redis disagree, Postgres
wins, and the reconciler (section 11) exists to find the disagreements.

## Design goals, non-goals, assumptions

Goals: never assign one driver to two riders; never charge a price that
cannot be reproduced from stored inputs; keep the "find candidates" path
fast enough to run on every request; make every state change explicit
and auditable.

Non-goals: real road routing, payments, maps UI, ML-based pricing,
shared/pooled rides. See the end of this document.

Capacity assumptions for one region (to be benchmarked, not promised):
50,000 online drivers, one location ping per driver every 4 seconds
(about 12,500 writes/s), 100 ride requests/s at peak, 50,000 open
WebSockets. These are sized to be large enough that naive designs
visibly fail, and small enough that one Redis primary and a handful of
Go replicas should hold them.

---

## 1. Two-tier geospatial index: hot positions in Redis, durable geography in PostGIS

**The problem:** a driver's position changes every few seconds and is
worthless seconds later. A rider request needs "who is near here, right
now" answered in milliseconds. Meanwhile some geographic facts are
durable, polygon-shaped, and queried by containment: is this pickup
inside our service area, is it inside the airport zone.

**Mechanism:** the two workloads get two stores.

- **Redis GEO** (`geo:avail:{region}`) holds only drivers who are
  currently AVAILABLE. `GEOADD` on each ping, `GEOSEARCH ... BYRADIUS
  ... ASC COUNT n WITHDIST` for candidate retrieval. Redis GEO is a
  sorted set whose score is a 52-bit geohash, so the same set also
  supports `ZREM` and `ZSCORE`, which section 3 exploits.
- **PostGIS** holds `service_areas` and `special_zones` as polygons with
  GiST indexes, resolved with `ST_Covers(polygon, point)`, plus sampled
  trip breadcrumbs (section 9). Zone lookups are cached in process by
  geohash cell, because polygon boundaries change rarely.

**Why not PostGIS for live positions:** 12,500 location updates per
second against a GiST-indexed table means constant index churn and
write amplification for data nobody needs after 15 seconds. It can be
made to work with unlogged tables and aggressive vacuuming, and a
reviewer will ask about it. The honest answer is that Redis is the
better-fitting tool for position, and PostGIS is the better-fitting tool
for geometry.

**Why `{region}` in the key:** a single Redis key cannot be sharded. All
keys carry a region hash tag, so one city is one slot (and Lua scripts
touching several keys in it stay legal under Redis Cluster), and a
second city is a second key on possibly a second node.

**Trade-off:** two stores means two things to keep consistent, which is
the whole reason section 11 exists.

## 2. Candidate retrieval and ranking: radius rings, then ETA, behind an interface

**Mechanism:** the matcher searches in expanding rings (default 1 km,
2 km, 4 km, 8 km). Each ring is one `GEOSEARCH` returning up to 20
candidates by straight-line distance. Straight-line distance is only a
filter. Candidates are then ranked by estimated pickup time from an
`ETAProvider` interface:

```
ETAProvider.Estimate(ctx, from, to Point) (seconds int, meters int, err error)
```

The v1 implementation is haversine distance times a circuity factor
(default 1.3) divided by an assumed average speed. A real routing engine
(OSRM, Valhalla) would slot in behind the same interface with no
changes to the matcher. Ties break on longest idle time, which spreads
work across drivers instead of always favoring whoever sits on a busy
corner.

**Trade-off:** haversine ETA is wrong near rivers, highways, and
one-way grids. The interface makes that a documented, replaceable
limitation, not a hidden one.

## 3. Atomic driver claim: one writer wins, enforced twice

**The race:** two requests, near the same driver, run at the same
moment. Both read that driver as the nearest. Both offer the ride.
Whichever accepts second finds the driver already committed elsewhere,
or worse, both succeed.

**Layer one: claim by removal.** A driver is a candidate only while
present in `geo:avail:{region}`. Claiming a driver is a Lua script:

1. `ZREM geo:avail:{region} driverId`
2. if it removed 1 member: set `driver:{id}` status to `OFFERED`, record
   `offer_id`, return 1
3. otherwise return 0 (someone else got there first)

`ZREM` returning 1 is the atomic "I won" signal. A claimed driver is
absent from the available set, so every later candidate query excludes
them automatically, with no per-candidate status check. The same script
shape (atomic check-and-act inside Redis) is the discipline GateKeeper
applies to rate-limit counters.

**Layer two: database backstop.** Redis state can be lost or wrong, so
Postgres refuses to represent the double-assignment even if Redis
allows it:

- a partial unique index on `offers(driver_id) WHERE status = 'PENDING'`
- a partial unique index on `trips(driver_id) WHERE status IN
  ('ACCEPTED','ARRIVED','IN_PROGRESS')`

A bug, a Redis flush, or a manual script cannot create a second active
assignment, because the insert fails. The Lua claim keeps contention
cheap and fast; the indexes keep correctness unconditional. This is the
same two-layer pattern as LedgerLine's row locks plus balance trigger.

**Release:** on decline, expiry, or rider cancel, a second script
re-adds the driver to the available set at their last known position
(stored in `driver:{id}`) only if their status is still `OFFERED` with
the matching `offer_id`. The `offer_id` check stops a late release from
resurrecting a driver who has since been given a different offer.

## 4. Offer lifecycle: sequential offers with deadlines that survive crashes

**Mechanism:** one offer at a time per trip, to the best-ranked
candidate, with a timeout (default 10 s). Decline or timeout releases
the driver and offers the next candidate, widening rings as needed,
until a budget runs out (default 8 offers or 90 s), after which the trip
moves to `NO_DRIVERS`.

**Deadlines:** a Redis sorted set `offers:deadline` (score = expiry
unix-ms, member = offer_id). A timer loop pops due members with `ZREM`;
only one worker's `ZREM` returns 1, so exactly one worker handles each
expiry. Redis keyspace expiry notifications are deliberately not used,
because they are fire-and-forget and lost if no subscriber is listening
at that instant.

**Accept versus expiry:** these two events can arrive within
milliseconds of each other. Both resolve through the same compare-and-
swap in Postgres: `UPDATE offers SET status = $new WHERE id = $1 AND
status = 'PENDING'`. Exactly one UPDATE affects a row. The loser reads
zero rows affected and backs out cleanly. There is no timing in which an
expired offer can be accepted or an accepted offer can expire.

**Matcher ownership:** new trips are published to a Redis Stream
(`stream:dispatch`) and consumed through a consumer group, so each trip
goes to one matcher worker. The worker holds a per-trip lease key with a
TTL, renewed while it works. If the worker dies, another reclaims the
stream entry with `XAUTOCLAIM` after the idle time passes. Every step
is a compare-and-swap, so a rare double-run is harmless.

**Known gap, stated openly:** creating the trip row in Postgres and
publishing to the stream are two writes with no shared transaction. A
crash between them leaves a SEARCHING trip nobody is working on. The
reconciler (section 11) re-enqueues any SEARCHING trip with no recent
offer activity. A transactional outbox (as in Dispatcher) would close
the gap exactly; the sweep was chosen to avoid another table and
another relay process, at the cost of up to one sweep interval of delay
in a rare failure.

**Why sequential and not broadcast:** offering one driver at a time
avoids several drivers racing to accept and keeps assignment fair and
predictable. The cost is latency when the first choice declines. See
"What I'd change" for batched matching.

## 5. Dispatch state machine: explicit transitions, enforced in code and in the database

| From | To | Actor | Trigger |
|---|---|---|---|
| REQUESTED | SEARCHING | system | trip persisted, enqueued |
| SEARCHING | OFFERED | matcher | driver claimed, offer created |
| OFFERED | SEARCHING | matcher | decline or expiry, driver released |
| OFFERED | ACCEPTED | driver | accept, driver bound to trip |
| SEARCHING | NO_DRIVERS | matcher | search budget exhausted |
| ACCEPTED | ARRIVED | driver | at pickup |
| ARRIVED | IN_PROGRESS | driver | rider on board |
| IN_PROGRESS | COMPLETED | driver | trip ended, final fare fixed |
| REQUESTED, SEARCHING, OFFERED, ACCEPTED, ARRIVED | CANCELLED | rider, driver, system | cancel |

`IN_PROGRESS -> CANCELLED` is intentionally absent: cancelling a ride
with a rider on board is a dispute flow, which is out of scope. Any
transition not in the table is rejected.

**Three enforcement layers:**

1. **Pure function in Go:** `Transition(from, event) (to, error)` is a
   lookup table with no side effects, tested exhaustively (every state
   against every event, asserting allowed ones succeed and all others
   fail).
2. **Compare-and-swap writes:** `UPDATE trips SET status = $new,
   version = version + 1 WHERE id = $1 AND status = $old AND version =
   $v`. Zero rows affected means someone else moved first.
3. **Database trigger:** a `BEFORE UPDATE` trigger on `trips` checks the
   `(old.status, new.status)` pair against an `allowed_transitions`
   table and raises on anything else. Code that bypasses the Go layer
   still cannot create an illegal state.

**Event history:** every transition appends a row to `trip_events`
(trip_id, from, to, actor, payload, created_at). That table is
append-only, enforced by a `BEFORE UPDATE OR DELETE` trigger, the same
immutability technique as LedgerLine's entries. Current state is a
projection; the event log is the history, and it is what answers "why
did this driver get this ride."

## 6. Pricing: surge as explicit, ordered, testable rules

**The principle:** the price a rider sees must be explainable from
stored inputs. "The algorithm decided" is not acceptable for a number
that affects money and disputes.

**Money arithmetic:** all amounts are integers in minor units (kobo,
cents). Multipliers are integers in basis points (10000 = 1.00x, 12500 =
1.25x). There are no floats anywhere in the pricing path, and there is
exactly one rounding point (round half up, after the multiplier is
applied), defined once and tested.

**Fare formula:**

```
metered   = per_km_minor * meters / 1000  +  per_min_minor * seconds / 60
surged    = round_half_up( (base_minor + metered) * multiplier_bps / 10000 )
fare      = max( min_fare_minor, surged ) + booking_fee_minor
```

The booking fee is deliberately outside the multiplier. Cities and
operators differ on this; the choice is explicit and configurable.

**The rules engine:** a rule set is an ordered list of rules, each a
small declarative object (id, type, parameters). The engine is a pure
function:

```
Evaluate(input PricingInput, rules RuleSet) Quote
```

Rule types in v1, evaluated in this order:

1. **RATIO_TIERS:** demand-to-supply ratio in a cell maps to a
   multiplier via tiers (for example below 1.0 gives 10000, 1.0 to 1.5
   gives 12000, 1.5 to 2.5 gives 15000, above 2.5 gives 20000).
2. **TIME_WINDOW:** a schedule-based floor (for example weekday
   evening peak).
3. **ZONE_OVERRIDE:** an operator-set multiplier for a cell or special
   zone (airport, event, storm).
4. **SMOOTHING:** limits movement versus the previous multiplier to a
   maximum step per interval, so the price does not oscillate between
   1.0x and 2.0x every 30 seconds.
5. **CLAMP:** floor of 10000, configurable cap (default 30000).

Rules 1 to 3 combine by taking the **maximum**, not the product.
Compounding independent surges multiplies surprises, and "highest
applicable" is easier to explain to a rider and to a regulator. This is
a decision, stated so it can be challenged.

**Explainability:** every quote carries `applied_rules[]`: rule id, the
input values it saw, and the effect it had. A support agent or a test
can replay the exact computation.

**Versioning:** rule sets are immutable and versioned
(`pricing_rule_sets`). A quote stores the `rule_set_version` it used.
Changing the rules next week never alters how last week's fare is
explained.

**Testing story:** the engine is table-driven and golden-file tested.
Each case is (input, rule set) -> expected quote JSON. Because the
function is pure, a property test can assert invariants: the fare never
falls below the minimum, multiplier is never above the cap, identical
input always yields identical output.

## 7. Surge calculation: a scheduled snapshot, deliberately simple

**Mechanism:** a scheduler (default every 30 s) computes per-cell supply
and demand and writes `surge:{cell}` (multiplier plus the inputs that
produced it) to Redis with a TTL.

- **Cells:** geohash precision 5 (roughly 5 km by 5 km), configurable.
  Cells are computed in process; no PostGIS call is needed for surge.
- **Demand:** requests created in the cell over the trailing window
  (default 5 min), tracked in a per-cell sorted set trimmed on write.
- **Supply:** available drivers currently in the cell. The calculator
  reads positions of the available set in pipelined batches and buckets
  them by cell. At 50,000 drivers that is a few dozen pipelined calls
  every 30 s, which is cheap, and far simpler than maintaining
  incrementally updated per-cell sets across every claim, release, and
  cell crossing. If it ever shows up in a profile, incremental counters
  are the optimization, and that is recorded as a known option.
- **Thin-market guard:** below a minimum demand sample (default 3
  requests), the ratio is ignored and surge stays 1.0x. A single
  request in a quiet cell must not look like a 5x demand spike.
- **Zero supply:** treated as the top tier, not a divide-by-zero.

**Fail-safe default:** if `surge:{cell}` is missing or expired, pricing
falls back to 1.0x and emits a metric. A stale calculator should never
produce a stuck high surge; it should degrade to the cheap, safe price
and alert.

## 8. Quote locking: the price shown is the price charged

`POST /v1/rides/quote` computes a quote for (pickup, dropoff), stores
it with its inputs, applied rules, rule set version, and an expiry
(default 2 minutes), and returns a `quote_id`. `POST /v1/rides` must
present that `quote_id`. The trip is created with the quote's price, and
the search that follows (which can take over a minute) never reprices
it. Expired quotes are rejected with a clear error, and the client
re-quotes.

Surge applies to the **upfront estimate** (distance and duration from
the ETA provider). Final-fare reconciliation against actual metered
distance after completion is a documented extension, not v1.

**Idempotency:** `POST /v1/rides` takes an `Idempotency-Key`, backed by a
unique constraint, so a double tap or a retried request returns the
existing trip instead of creating a second one. A partial unique index
also enforces at most one active trip per rider.

## 9. Real-time location streaming: stateless gateways, latest-wins delivery

**Sockets:**

- `WS /v1/ws/driver`: driver sends `location` messages, server pushes
  `offer`, `offer_expired`, `trip_update`.
- `WS /v1/ws/rider?trip_id=`: rider receives `driver_location` and
  `trip_update` for that trip only.

**Gateway design:** each connection runs a read pump and a write pump
goroutine. The gateways are stateless: when a driver pings, the handler
writes Redis (section 1) and, if the driver is on a trip, publishes to
`trip:{id}:loc`. Whichever gateway holds the rider's socket is
subscribed to that channel and forwards. A rider and driver on
different gateway instances need no coordination beyond Redis pub/sub.

**Backpressure, and why dropping is correct here:** each connection has
a bounded send buffer. Location updates are latest-wins: if the buffer
is full, older location frames are dropped in favor of the newest,
because a stale position has no value. A connection that cannot keep up
even so is closed, and the client reconnects. Control messages (offers,
state changes) are never dropped; they use a separate, small, reliable
path.

**Hygiene:**

- ping/pong heartbeats with read deadlines; dead peers are reaped
- max message size, JSON schema validation, origin allowlist
- per-driver rate limit (token bucket, default 1 message/s)
- ordering: each location carries a client timestamp and sequence; the
  server ignores anything not newer than the last accepted
- plausibility: reject coordinates out of range, and speeds between
  pings above a threshold (default 200 km/h), a basic guard against
  GPS noise and trivial spoofing
- auth: short-lived token validated at upgrade; the connection is bound
  to one identity and role

**Breadcrumbs:** while a driver is on a trip, one position per 15 to 30
seconds is batched into PostGIS (`COPY`) for dispute resolution and
distance verification. Available drivers are never written to Postgres.
Location data is sensitive: retention is configurable (default 30 days)
and enforced by a scheduled purge.

## 10. Failure modes and recovery

| Failure | What happens | Why it is safe |
|---|---|---|
| Matcher dies mid-search | Stream entry reclaimed by another worker after idle time | Every step is a CAS; replay is idempotent |
| Matcher dies after claiming a driver | Offer row is PENDING in Postgres with a deadline | Startup rebuilds `offers:deadline`; expiry releases the driver |
| Redis flushed or failed over | Available set empties | Drivers reappear on next ping; deadlines rebuilt from Postgres |
| Redis claim says free, Postgres says busy | Insert hits partial unique index | The DB refuses the double assignment |
| Accept and expiry arrive together | Both attempt `status = 'PENDING'` CAS | Exactly one affects a row |
| Gateway restarts | Sockets drop, clients reconnect | State is in Postgres and Redis, not in the gateway |
| Driver vanishes (app killed) | No pings; seen-timestamp goes stale | Sweeper removes stale members from the available set |
| Trip row written but never enqueued | SEARCHING with no activity | Reconciler re-enqueues |
| Surge calculator stops | `surge:*` TTLs expire | Pricing falls back to 1.0x and alerts |

Stale-driver sweeping: a companion sorted set `drivers:seen:{region}`
(score = last ping time) is trimmed on a short interval. Members older
than the freshness window (default 15 s) are removed from
`geo:avail:{region}` as well. Candidate queries also filter on
freshness, so a ghost driver cannot be offered a ride in the gap between
sweeps.

## 11. Reconciliation: Redis is allowed to be wrong, as long as we notice

A scheduled job compares the ephemeral view against Postgres and records
every run in `reconciliation_runs` (including clean ones):

1. **Stuck drivers:** Redis says OFFERED or ON_TRIP but no PENDING offer
   or active trip exists in Postgres. The driver is released.
2. **Orphan assignments:** an active trip exists but the driver appears
   AVAILABLE in Redis. Flagged loudly; this should be impossible.
3. **Stuck trips:** SEARCHING beyond the search budget, or OFFERED with
   an expired offer. Re-enqueued or moved to NO_DRIVERS.
4. **Deadline set drift:** PENDING offers in Postgres missing from
   `offers:deadline`. Re-added.
5. **Surge input sanity:** recount supply for sampled cells and compare
   against the calculator's stored inputs.

This is the same "fast derived view, authoritative store, scheduled
proof they agree" shape as LedgerLine's balance reconciliation, applied
to assignment state instead of money.

## 12. Observability and targets

Metrics: request-to-first-offer latency (p50, p95), time to accepted
match, offer acceptance rate, offers per trip, NO_DRIVERS rate, candidate
query latency, location ingest rate, active sockets, dropped location
frames, surge multiplier per cell, fallback-to-1.0x count,
reconciliation findings by type.

Targets to validate by benchmark, not claims: candidate retrieval p99
under 10 ms at 50,000 available drivers, request-to-first-offer p95
under 500 ms excluding network, claim contention test passes with 0
double assignments across 1,000 concurrent requests.

Structured logs carry `trip_id`, `offer_id`, `driver_id`, and
`request_id` on every line.

## 13. Data model summary

- `drivers` (id, status, vehicle, created_at)
- `trips` (id, rider_id, driver_id, status, version, pickup, dropoff as
  PostGIS points, quote_id, fare_minor, currency, idempotency_key,
  timestamps)
- `trip_events` (append-only)
- `offers` (id, trip_id, driver_id, status, expires_at, created_at)
- `quotes` (id, inputs, applied_rules, rule_set_version, fare breakdown,
  expires_at)
- `pricing_rule_sets` (version, rules JSON, created_by, created_at;
  immutable)
- `service_areas`, `special_zones` (PostGIS polygons, GiST indexes)
- `driver_breadcrumbs` (trip_id, point, recorded_at; partitioned by time)
- `allowed_transitions` (from, to)
- `reconciliation_runs`

## 14. Security notes

JWT with role claims (rider, driver, admin) is a placeholder auth layer,
enough to demonstrate authorization boundaries (a driver cannot accept
another driver's offer, a rider cannot read another rider's trip).
Admin-only endpoints cover pricing rule sets and reconciliation history.
Production identity, device attestation, and anti-GPS-spoofing beyond
the plausibility check are out of scope.

## What is out of scope, and why

- **Real routing and traffic ETA.** Interface exists; implementation is a
  separate project.
- **Payments and settlement.** Tessera produces a fare; moving money is
  LedgerLine's problem.
- **Driver and rider apps, maps UI.** A Go driver simulator replaces them.
- **ML or learned pricing.** The point is explicit, testable rules.
- **Pooled rides, scheduled rides, multi-stop.** Each is its own
  matching problem.
- **Multi-region deployment.** Keys are region-scoped so it is
  possible, but only one region is implemented.

## What I'd change with more time

- **Batched matching.** Collect requests and available drivers over a
  short window (1 to 2 s) and solve the assignment globally (Hungarian
  algorithm or a min-cost flow). Greedy nearest-driver is locally
  optimal and globally suboptimal. The matcher sits behind a `Strategy`
  interface so this swaps in.
- **H3 hexagons instead of geohash cells** for surge, since hexagons
  have uniform neighbor distance and avoid geohash edge effects.
- **Transactional outbox** to remove the dual-write gap in section 4.
- **Incremental per-cell supply counters** if the snapshot scan ever
  profiles badly.
- **Final-fare reconciliation** against breadcrumb distance after
  completion.