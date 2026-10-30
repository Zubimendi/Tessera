# CURSOR_CONTEXT: Tessera

Read this first, then `docs/ARCHITECTURE.md` in full before writing any
code. This is a build spec.

## Status

Design only. No implementation code exists. Everything below is work to
be written. Do not claim anything works until it has been run; where
tests are listed, they must actually pass before the phase is marked
done.

## Working agreement

- When modifying an existing file, show only the changed blocks with
  exact placement instructions (file, function, before/after anchor),
  not full-file rewrites.
- Money is integer minor units, multipliers are integer basis points.
  No floats in the pricing path. Ever.
- Postgres is the source of truth. Redis may be wrong and must be
  recoverable.
- Do not change a decision listed under "Decisions not to relitigate"
  without raising it first.

## Stack

Go 1.22+, PostgreSQL 16 with PostGIS (`postgis/postgis:16-3.4` image),
Redis 7, Docker Compose. Suggested libraries: `pgx/v5`, `go-redis/v9`,
`coder/websocket` (or `gorilla/websocket`), `golang-jwt/jwt/v5`,
`prometheus/client_golang`, `testcontainers-go` (optional, for
integration tests), stdlib `net/http` ServeMux with Go 1.22 method
patterns.

## Repo layout

```
cmd/api/              REST + WebSocket gateway
cmd/matcher/          offer loop workers + deadline timer
cmd/surge/            scheduled surge calculator
cmd/reconciler/       scheduled invariant checks
cmd/driversim/        driver + rider simulator (load + demo tool)
internal/geo/         Redis geo index, claim/release Lua, stale sweeper
internal/dispatch/    state machine, matcher, offers, strategy interface
internal/pricing/     rules, engine, quotes, surge reader
internal/eta/         ETAProvider interface + haversine implementation
internal/ws/          gateway hub, connections, pub/sub fan-out
internal/store/       pgx repositories
internal/auth/        JWT + role middleware
internal/config/      env config
internal/obs/         metrics, structured logging
migrations/           0001_init.sql (schema, triggers, indexes)
docs/                 PRD, ARCHITECTURE, TESTING, STORY, CURSOR_CONTEXT
postman/              collection + environment (see Extras)
```

## Build order (each phase has an acceptance gate)

**Phase 1: schema.** `migrations/0001_init.sql`: all tables in
ARCHITECTURE section 13, PostGIS extension, GiST indexes, the partial
unique indexes (offers and trips), `allowed_transitions` seed, the
transition-validation trigger on `trips`, the append-only trigger on
`trip_events`. Gate: raw SQL can create a legal trip, and fails on an
illegal transition, a second active trip for one driver, and any
UPDATE/DELETE on `trip_events`.

**Phase 2: geo index + atomic claim.** `internal/geo`: GEOADD on ping,
candidate search by ring, claim and release as Lua scripts (release
checks `offer_id`), freshness filter, stale sweeper. Gate: a test with
1,000 goroutines claiming one driver yields exactly one success.

**Phase 3: state machine.** Pure transition function plus CAS
repository methods that append to `trip_events` in the same
transaction. Gate: exhaustive table test of every state against every
event.

**Phase 4: matcher + offers.** Stream consumer group, per-trip lease,
ring expansion, ETA ranking, offer creation, `offers:deadline` timer
(pop with ZREM), accept/decline/expiry via CAS, startup rebuild of the
deadline set from Postgres, search budget to NO_DRIVERS. Gate: accept
versus expiry race test (run many iterations, exactly one outcome each
time); kill-the-matcher test recovers.

**Phase 5: pricing engine.** `internal/pricing`: rule types
(RATIO_TIERS, TIME_WINDOW, ZONE_OVERRIDE, SMOOTHING, CLAMP), pure
`Evaluate`, quote persistence with `applied_rules` and
`rule_set_version`, immutable versioned rule sets. Gate: golden-file
tests plus property tests (fare >= min fare, multiplier <= cap,
determinism).

**Phase 6: surge calculator.** Scheduled snapshot of supply and demand
per geohash cell, thin-market guard, zero-supply handling, TTL'd
`surge:{cell}`, 1.0x fallback with metric. Gate: scripted scenarios
produce expected multipliers; stopping the calculator degrades to 1.0x.

**Phase 7: REST API + auth.** Endpoints below. JWT roles. Idempotency-Key
on ride creation. Gate: role boundary tests.

**Phase 8: WebSocket gateway.** Driver and rider sockets, pub/sub
fan-out, bounded buffers with latest-wins location frames, heartbeats,
rate limit, plausibility checks, breadcrumb batching to PostGIS. Gate:
slow-consumer test does not stall other connections; reconnect works.

**Phase 9: reconciler.** The five checks in ARCHITECTURE section 11,
every run persisted. Gate: deliberately corrupt Redis state and see each
check catch it.

**Phase 10: driversim + docs.** Simulator that spawns N drivers moving
along random paths and M riders requesting rides; TESTING.md written
from what was actually run.

## API surface

- `POST /v1/rides/quote`
- `POST /v1/rides` (Idempotency-Key, quote_id)
- `GET /v1/rides/{id}`, `POST /v1/rides/{id}/cancel`
- `POST /v1/drivers/{id}/online`, `POST /v1/drivers/{id}/offline`
- `POST /v1/offers/{id}/accept`, `POST /v1/offers/{id}/decline`
- `POST /v1/trips/{id}/arrived`, `/start`, `/complete` (driver)
- `WS /v1/ws/driver`, `WS /v1/ws/rider?trip_id=`
- Admin: `GET/POST /v1/admin/pricing/rule-sets`,
  `GET /v1/admin/surge/cells`, `GET /v1/admin/reconciliation`
- `GET /healthz`, `GET /metrics`

## Tests that must exist (and be run)

1. 1,000-way claim contention: exactly one winner, zero double
   assignments.
2. Accept-versus-expiry race, repeated: exactly one outcome.
3. Partial unique index backstop: bypass Redis, attempt a second active
   assignment via SQL, expect failure.
4. Illegal transition via raw SQL rejected by trigger.
5. `trip_events` UPDATE/DELETE rejected.
6. Pricing golden files and property tests.
7. Quote expiry rejected; repriced search never happens.
8. Idempotent ride creation under concurrent duplicate requests.
9. Redis flushed mid-search: drivers repopulate, offers recovered.
10. Slow WebSocket consumer does not block others.
11. Reconciler detects injected drift of each type.

## Decisions not to relitigate

- Redis for live positions, PostGIS for polygons and breadcrumbs.
- Claim by atomic ZREM from the available set, plus Postgres partial
  unique indexes. Both layers stay.
- Sequential offers v1; keep the `Strategy` interface so batching can
  replace it later.
- Integer money, basis-point multipliers, one rounding point.
- Rules combine by max, not product.
- Quote locked at request time; no mid-search repricing.
- Fail-safe to 1.0x surge when data is missing.
- Offer deadlines in a Redis sorted set, not keyspace notifications.
- Append-only `trip_events` enforced by trigger.

## Open decisions to confirm with the owner

- WebSocket library (`coder/websocket` vs `gorilla/websocket`).
- Surge cell scheme: geohash precision 5 (default) vs adopting H3 now.
- Whether to ship the transactional outbox in v1 or keep the
  reconciler sweep (default: sweep).

## Extras (optional deliverables, do after Phase 10)

### A. Postman collection

Create `postman/Tessera.postman_collection.json` (Collection v2.1
schema) and `postman/Tessera.local.postman_environment.json`.

- Environment variables: `baseUrl`, `riderToken`, `driverToken`,
  `adminToken`, `driverId`, `quoteId`, `tripId`, `offerId`,
  `idempotencyKey`.
- Folders: Auth/Setup, Quotes, Rides (rider), Driver Actions
  (online/offline, accept/decline, arrived/start/complete), Admin
  (pricing rule sets, surge cells, reconciliation), Health/Metrics.
- Test scripts on each request: assert status code, parse the response,
  and store IDs with `pv.environment.set(...)` so the next request works
  without copy-paste. Use a pre-request script to generate a fresh
  `idempotencyKey`.
- Include an ordered "Happy path" flow: quote -> create ride -> driver
  online -> accept -> arrived -> start -> complete.
- Include negative cases: expired quote, duplicate Idempotency-Key,
  driver accepting someone else's offer (expect 403), illegal
  transition (expect 409).
- WebSocket: Postman's WebSocket requests are saved separately and, as
  far as I know, do not export reliably inside a v2.1 collection file.
  Do not depend on them. Document the WS flow in the collection
  description and rely on `cmd/driversim` (or `websocat`) for socket
  testing. Verify this against current Postman behavior before
  promising otherwise.
- Validate the JSON by importing it into Postman before committing.

### B. Medium thumbnail

Produce a cover image for the article.

- Sizes: around 1400x788 (16:9) for Medium, and 1200x627 for the
  LinkedIn post image. Platform specs change, so verify the current
  recommendations before finalizing.
- Concept: dark map-style background with a faint grid of tiles, a few
  glowing driver dots, one rider pin with a ring, a small price chip
  reading "1.4x", and the title "Tessera" with the subtitle
  "Dispatch, priced in the open". Keep text large enough to read at
  thumbnail size and keep the key content inside the central safe area
  (platforms crop edges).
- Implementation options: write an SVG or HTML file and export to PNG
  with a headless browser script, or write an image-generation prompt
  using the concept above. Avoid logos, real map tiles, or any
  third-party brand marks.
- Provide a light and a dark variant if easy.

### C. Tags

Medium (max 5 tags, pick these): `Golang`, `System Design`, `PostGIS`,
`WebSockets`, `Backend Development`.

LinkedIn hashtags: `#golang` `#systemdesign` `#backendengineering`
`#postgis` `#redis` `#websockets` `#distributedsystems`
`#softwareengineering` `#buildinpublic`

Optional extras if room: `#postgresql`, `#geospatial`, `#dispatch`.

## Priority order if time is short

1. Phases 1 to 4 (schema, claim, state machine, matcher): this is the
   core claim of the project.
2. Phase 5 (pricing engine with golden tests): the second pillar.
3. Phase 7 and 10 (API and simulator) so it can be demonstrated.
4. Everything else.