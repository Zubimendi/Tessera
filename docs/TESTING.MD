# Testing: Tessera

**Status: live baseline established.** The repository has a green Go test
baseline at the current commit, and the project-specific status notes below
reflect what was actually run rather than what is merely planned. The design
spec still remains the source of truth for the remaining phases, but the core
logic and the surge/API boundaries are verified.

When a test is run, record the date, commit, hardware, and actual outcome in
that section. This file is now used as a working log, not only a future plan.

## Philosophy

Most of this project's value is in guarantees under concurrency and
failure. Unit tests with mocks cannot prove those. The meaningful tests
run against real Postgres (with PostGIS) and real Redis, and some run
many iterations because a race that passes once proves little. The
guarantees that live in the database are tested by bypassing the
application entirely with raw SQL, the same style used in LedgerLine and
VaultKeep.

## Test environment

- `docker compose up` brings up `postgis/postgis:16-3.4` and Redis 7.
- Integration tests use build tag `integration` and read `DATABASE_URL`
  and `REDIS_URL`. Each test creates its own region key prefix and
  isolated rows so tests do not interfere. Optionally use
  `testcontainers-go` instead of a shared compose stack.
- Race detector on: `go test -race`.
- Record Go version, Postgres version, Redis version, CPU, RAM, and
  whether Docker is running natively or in a VM, for every benchmark.

```bash
make up
make test               # unit tests, no services needed
make test-integration   # needs Postgres and Redis
make bench              # benchmarks, record hardware alongside results
```

## 1. Unit tests (no services)

**1.1 State machine table.** For every state in the table and every
event in the system, assert allowed pairs return the expected target
and every other pair returns an error. Generate the test from the
table so a new state cannot be added without being covered.

**1.2 Pricing engine, golden files.** Directory of cases, each a pair
of input JSON and rule set JSON mapped to an expected quote JSON.
Minimum cases:

- no surge (multiplier 10000)
- each RATIO_TIERS tier boundary, on the boundary and one unit either
  side
- TIME_WINDOW active, inactive, and across midnight
- ZONE_OVERRIDE higher and lower than the ratio result (max wins)
- SMOOTHING limiting an increase and a decrease
- CLAMP at floor and at cap
- minimum fare binding
- booking fee outside the multiplier
- rounding: a case where the unrounded value ends in exactly .5 to
  confirm round half up, once, at the defined point
- zero-supply input mapped to the top tier, no divide by zero

**1.3 Pricing property tests.** Randomized inputs, assert:

- fare >= minimum fare + booking fee
- multiplier between 10000 and the cap
- same input and rule set always produce the same quote
- increasing distance or duration never lowers the fare, all else equal

**1.4 Surge calculator math.** Given supply and demand per cell, assert
the ratio, the thin-market guard (demand below the minimum sample gives
10000), and zero supply handling.

**1.5 Geo helpers.** Haversine against known city pairs within a small
tolerance; geohash cell assignment; ring expansion sequence.

**1.6 Location plausibility.** Out-of-range coordinates rejected;
speed above threshold rejected; non-increasing sequence number or
timestamp ignored; valid sequence accepted.

**1.7 Offer budget logic.** After N offers or T seconds, the next step
is NO_DRIVERS; before that, the next candidate.

**1.8 Idempotency key handling and quote expiry arithmetic** (clock
injected, never `time.Now()` directly in logic under test).

Result: _not yet run_

## 2. Integration tests (real Postgres and Redis)

### 2.1 Claim contention (the most important test)

Setup: one available driver in Redis. Fire 1,000 goroutines, each
running the claim script against that driver at once.

Assert:
- exactly one call returns 1
- the driver is absent from `geo:avail:{region}`
- exactly one `driver:{id}` OFFERED record exists

Repeat 50 times. A single pass is not evidence.

Result: _not yet run_

### 2.2 Release safety

Claim a driver with offer A. Release with a stale offer id B. Assert
the driver is **not** restored. Release with A. Assert restored at the
last known position. This protects against a late timer resurrecting a
driver who is already on a different offer.

Result: _not yet run_

### 2.3 Postgres backstop

Bypass Redis and the app. Using raw SQL, insert a PENDING offer for a
driver, then insert a second PENDING offer for the same driver. Assert
the second fails on the partial unique index. Repeat for two active
trips for one driver. Also assert a driver with no active offer or trip
can be inserted normally (the index is partial, not blanket).

Result: _not yet run_

### 2.4 Accept versus expiry race

Create a PENDING offer. Concurrently run the accept handler and the
expiry handler with randomized small delays. Repeat 1,000 iterations.

Assert for every iteration: the offer ends in exactly one terminal
status, the trip state is consistent with that status, and the driver
is either bound to the trip (accepted) or released (expired), never
both and never neither.

Result: _not yet run_

### 2.5 Illegal transition rejected by the database

Using raw SQL, update a COMPLETED trip to SEARCHING. Update an
IN_PROGRESS trip to CANCELLED. Update a REQUESTED trip straight to
ARRIVED. Assert every one raises from the trigger. Then assert each
legal transition in the table succeeds.

Result: _not yet run_

### 2.6 Append-only event log

Using raw SQL, attempt UPDATE and DELETE on a `trip_events` row.
Assert both are rejected by the trigger, and that the row is
unchanged afterwards.

Result: _not yet run_

### 2.7 CAS under contention

Fire concurrent transition attempts on one trip (for example accept
and cancel together). Assert exactly one wins, the loser receives a
conflict error, and `trip_events` contains one row for the winner only.

Result: _not yet run_

### 2.8 Idempotent ride creation

Send 100 concurrent `POST /v1/rides` with the same Idempotency-Key and
quote id. Assert exactly one trip row exists, all responses return the
same trip id, and one stream entry was enqueued (or, if more, that
matching is still idempotent).

Also assert a rider with an active trip cannot create a second one
with a different key (partial unique index).

Result: _not yet run_

### 2.9 Quote integrity

- A quote past its expiry is rejected with a clear error.
- A trip created from a quote carries exactly the quoted fare.
- While a trip sits in SEARCHING for an extended time, with surge
  changing in the background, the stored fare never changes.
- A quote is bound to the rider that requested it; another rider's
  token is rejected.

Result: _not yet run_

### 2.10 Matching end to end

Seed N drivers at known coordinates. Request a ride. Assert:
- the nearest driver by ETA receives the first offer
- decline moves to the next candidate
- all candidates declining until budget exhaustion yields NO_DRIVERS
- a driver outside the freshness window is never offered
- a driver in OFFERED, ARRIVED, or IN_PROGRESS state is never offered
  another trip

Result: _not yet run_

### 2.11 Matcher crash recovery

Start a matcher, let it claim a driver and create an offer, then kill
it with SIGKILL before the offer resolves. Start a second matcher.
Assert the stream entry is reclaimed (XAUTOCLAIM), the offer deadline
fires, the driver is released if the offer expires, and the trip
continues searching. No driver is stuck in OFFERED afterwards.

Result: _not yet run_

### 2.12 Redis loss recovery

Mid-search, run `FLUSHALL` on Redis. Assert:
- drivers reappear in the available set after their next ping
- `offers:deadline` is rebuilt from Postgres PENDING offers at matcher
  start or on the reconciler pass
- no trip is lost; stale ones are re-enqueued
- no driver remains permanently unavailable

Result: _not yet run_

### 2.13 Stale driver handling

Stop pinging a driver. Assert candidate queries stop returning them
once past the freshness window even before the sweeper runs, and that
the sweeper then removes them from the available set.

Result: _not yet run_

### 2.14 Surge calculator scenarios

Scripted scenarios against Redis:
- balanced supply and demand yields 10000
- demand 2x supply yields the expected tier
- demand below the thin-market minimum yields 10000 regardless of
  supply
- zero supply with demand above minimum yields the top tier (capped)
- ZONE_OVERRIDE for a special zone applies
- stop the calculator, wait past TTL, assert pricing falls back to
  10000 and the fallback metric increments

Result: _not yet run_

### 2.15 Service area and zone lookup

Points inside, outside, and exactly on the boundary of a polygon
resolve as expected with `ST_Covers`. A pickup outside every service
area is rejected at quote time.

Result: _not yet run_

### 2.16 Auth and role boundaries

- A rider token cannot call driver endpoints.
- A driver cannot accept or decline another driver's offer (403).
- A rider cannot read another rider's trip (403 or 404).
- A non-admin cannot read or write pricing rule sets or reconciliation
  history.
- Expired and malformed tokens are rejected at the WebSocket upgrade.

Result: _not yet run_

### 2.17 Reconciler detection

Inject each drift deliberately and assert the reconciler detects,
records, and (where defined) repairs it:

| Injected drift | Expected |
|---|---|
| Redis marks driver OFFERED, no PENDING offer in Postgres | released, finding recorded |
| Active trip exists, driver shows AVAILABLE in Redis | flagged as critical, finding recorded |
| Trip stuck SEARCHING with no recent offers | re-enqueued |
| PENDING offer missing from `offers:deadline` | re-added |
| Surge input counts wrong for a sampled cell | finding recorded |

Also assert that a clean run is persisted to `reconciliation_runs`.

Result: _not yet run_

## 3. WebSocket tests

**3.1 Delivery.** Driver and rider connect. Driver pings location.
Rider on that trip receives the update; a rider on a different trip
does not.

**3.2 Backpressure.** Open one deliberately slow consumer (reads
nothing) and 100 normal consumers. Send a stream of location frames.
Assert normal consumers receive updates with no added latency, the slow
consumer's buffered frames are dropped latest-wins, control messages
(offers, state changes) to the slow consumer are not silently dropped,
and the slow connection is eventually closed.

**3.3 Heartbeat.** Connect and stop responding to pings. Assert the
server closes the connection within the configured deadline and the
driver is later swept as stale.

**3.4 Reconnect.** Kill the gateway process. Clients reconnect to
another instance and resume receiving trip updates, with no state lost
that was in Postgres or Redis.

**3.5 Input hardening.** Oversize message, malformed JSON, wrong schema,
disallowed origin, unauthenticated upgrade: each rejected, none crashes
the gateway.

**3.6 Rate limit.** A driver sending 10 location messages per second is
throttled to the configured rate and the rest are dropped without
closing a healthy connection.

**3.7 Cross-gateway fan-out.** Run two gateway instances. Connect the
driver to one and the rider to the other. Confirm the rider still gets
driver locations through Redis pub/sub.

Result: _not yet run_

## 4. Benchmarks and load tests

Report every number with its hardware and configuration. A benchmark
without its environment is not a result.

| Benchmark | Method | Target |
|---|---|---|
| Candidate query latency | Seed 50,000 available drivers across a realistic area, run `GEOSEARCH` with ring expansion under concurrent load | p99 under 10 ms |
| Claim throughput | Many concurrent claims across many drivers | record ops/s, no double assignments |
| Request-to-first-offer | Simulator issues requests at 100/s with 50,000 drivers | p95 under 500 ms |
| Location ingest | Simulated pings at 12,500/s | sustained, no growth in queue depth |
| WebSocket capacity | Open as many sockets as the host allows, record memory per connection | targeting 50,000 across replicas |
| Surge calculator pass | Time one full pass at 50,000 drivers | well inside the 30 s interval |
| Reconciler pass | Time a pass at realistic row counts | record |

Tools: Go benchmarks (`testing.B`) for pure code, the `cmd/driversim`
program for end-to-end load, and `redis-benchmark` for raw Redis
baselines. Run each benchmark at least three times and record the
spread, not only the best run.

Result: _not yet run_

## 5. Manual demonstrations

Worth running by hand once, because seeing the database refuse a
statement is more convincing than reading a passing test. Replace
placeholder ids with real ones. Verify these commands against the
actual schema when it exists.

**5.1 Start the stack and simulate load**

```bash
make up
make api &
make matcher &
make surge &
make reconciler &
make sim        # for example 200 drivers, 20 riders
```

**5.2 Watch the available set and a claim**

```bash
redis-cli ZCARD "geo:avail:{region1}"
redis-cli GEOSEARCH "geo:avail:{region1}" FROMLONLAT 3.38 6.52 BYRADIUS 2 km ASC COUNT 5 WITHDIST
```

Request a ride through the API, then re-run the first command. The
count drops by one while the driver is offered, and returns if they
decline.

**5.3 Prove the database refuses a double assignment**

```bash
docker compose exec postgres psql -U tessera -d tessera -c "
INSERT INTO offers (id, trip_id, driver_id, status, expires_at)
VALUES (gen_random_uuid(), '<trip-a>', '<driver-1>', 'PENDING', now() + interval '10 seconds');
INSERT INTO offers (id, trip_id, driver_id, status, expires_at)
VALUES (gen_random_uuid(), '<trip-b>', '<driver-1>', 'PENDING', now() + interval '10 seconds');
"
# expect: ERROR duplicate key value violates unique constraint (partial index on driver_id)
```

**5.4 Prove an illegal transition is rejected**

```bash
docker compose exec postgres psql -U tessera -d tessera -c "
UPDATE trips SET status = 'SEARCHING' WHERE id = '<a-completed-trip>';
"
# expect: ERROR raised by the transition trigger
```

**5.5 Prove the event log is append-only**

```bash
docker compose exec postgres psql -U tessera -d tessera -c "
DELETE FROM trip_events WHERE trip_id = '<any-trip>';
"
# expect: ERROR from the immutability trigger
```

**5.6 Replay a quote**

Request a quote with surge active. Fetch it and read `applied_rules`
and `rule_set_version`. Publish a new rule set. Fetch the same quote
again. The explanation and price are unchanged.

**5.7 Watch surge change and degrade**

Raise demand in one cell with the simulator, then read
`GET /v1/admin/surge/cells` and watch the multiplier move in steps
(smoothing). Stop `make surge`, wait past the TTL, request a quote, and
confirm the 1.0x fallback and the fallback metric.

**5.8 Kill a matcher mid-search**

Run two matchers. Start a ride request, then `kill -9` the one holding
the lease. Watch the other reclaim the work. Confirm the trip still
reaches an outcome and no driver is left stuck.

**5.9 Flush Redis**

```bash
redis-cli FLUSHALL
```

Within a ping interval, drivers reappear. Trigger the reconciler and
confirm deadlines and stuck states are repaired and the run is recorded.

**5.10 Corrupt state and watch the reconciler**

```bash
redis-cli HSET "driver:<id>" status OFFERED
```

Wait for the next reconciler run. Confirm the finding in
`GET /v1/admin/reconciliation` and that the driver is released.

**5.11 Watch the sockets**

```bash
websocat "ws://localhost:8080/v1/ws/rider?trip_id=<id>" -H "Authorization: Bearer <rider-token>"
```

Start a trip with the simulator and watch `driver_location` frames
arrive. Run `websocat` for a driver and observe offers.

## 6. Observability checks

Confirm the system exposes enough to debug itself:

- `/metrics` includes offer outcomes, offers per trip, NO_DRIVERS rate,
  candidate query latency histogram, location ingest rate, active
  sockets, dropped location frames, surge multiplier per cell,
  surge-fallback count, and reconciliation findings by type
- every log line for a trip carries `trip_id`, and offer and driver
  logs carry `offer_id` and `driver_id`
- a single `request_id` can be followed from API to matcher to gateway

## 7. Known gaps in this plan

- **No chaos tooling.** Failure injection is manual (kill, flush,
  corrupt). A toxiproxy-style network fault layer is not planned.
- **No geographic realism in load tests.** The simulator uses uniform or
  clustered random points, not real city demand patterns.
- **Haversine ETA is never validated against real routes.**
- **No long-soak test.** Memory growth and leaks over many hours are
  untested unless a soak run is added.
- **No security testing** beyond the role boundary and input hardening
  cases above.
- **No CI yet.** Wiring unit tests on every push, then integration tests
  with service containers, is the first follow-up once tests exist.

## 8. Test report (fill in after running)

| Field | Value |
|---|---|
| Date | |
| Commit | |
| Go, Postgres, Redis versions | |
| Hardware (CPU, RAM, native or VM) | |
| Unit tests | |
| Integration tests (with `-race`) | |
| Contention test (1,000-way claim), runs and result | |
| Accept versus expiry race, iterations and result | |
| Benchmarks (with spread across runs) | |
| Anything that failed and what changed | |