# The story: Tessera

## Short version (bio line or post opener)

Designed a ride and delivery dispatch engine in Go where a driver can
never be assigned two trips at once (enforced by an atomic Redis claim
and by database constraints), and where surge pricing is a set of
explicit, versioned, testable rules instead of a magic number.

## Longer version (LinkedIn or Medium)

Dispatch is a favorite system design interview question because it
looks simple and is not. "Find the nearest driver" hides at least four
hard problems: finding someone nearby fast, making sure two riders do
not get the same driver, deciding what the ride costs when everyone
wants one at once, and showing a moving dot on a map without melting
your servers.

I built Tessera to work through all four on purpose.

For location, I split the workload. Live driver positions change every
few seconds and are worthless soon after, so they live in Redis geo
sets. Durable geography, like service areas and airport zones, lives in
PostGIS as real polygons. Each store does the job it is good at.

The double-assignment problem was the part I found most interesting.
Two requests can pick the same nearest driver in the same millisecond.
In Tessera, a driver is only a candidate while they sit in the
available set, and claiming a driver is a single atomic removal from
that set. Whoever removes them wins, everyone else moves on to the next
candidate. Then, because Redis can be flushed or wrong, Postgres
independently refuses to store two active assignments for one driver
through partial unique indexes. A fast layer for contention, an
unconditional layer for correctness.

Pricing is where I wanted to avoid the usual black box. Surge in
Tessera is an ordered list of declarative rules evaluated by a pure
function. Money is integer minor units, multipliers are integer basis
points, there are no floats, and every quote records which rules fired
and which version of the rule set produced it. If someone disputes a
fare, you can replay the exact computation. The price shown is locked
into a quote, so a rider is never repriced while a search runs.

The trip itself is a state machine whose transitions are enforced three
times: a pure function in Go, compare-and-swap writes, and a database
trigger. Every transition lands in an append-only event log, so "why
did this driver get this ride" has an answer.

[Add measured results here after benchmarking: candidate query p99,
request-to-first-offer p95, and the result of the 1,000-way claim
contention test. Do not publish targets as results.]

## Interview talking points

- **"How do you stop two riders getting the same driver?"** Claim by
  atomic removal from the available geo set (Lua), backed by partial
  unique indexes in Postgres. Fast path plus unconditional backstop. I
  can explain why either one alone is not enough.

- **"Why Redis for positions and PostGIS for zones?"** Different
  workloads. Positions are high-write and short-lived; zones are
  durable and queried by containment. Putting live positions in a
  GiST-indexed table would create write amplification for data that
  expires in seconds.

- **"What happens if the matcher crashes mid-offer?"** Offers and
  deadlines are durable in Postgres; the Redis deadline set is rebuilt
  on startup; every step is a compare-and-swap, so replays are safe.
  There is one honest gap, the dual write between creating a trip and
  enqueueing it, and I can say how the reconciler covers it and what an
  outbox would change.

- **"Why not just use a magic surge multiplier?"** Because a price must
  be explainable. Rules are explicit, versioned, and replayable, and
  combine by maximum rather than product so surprises do not compound.
  Smoothing prevents oscillation, and a thin-market guard prevents one
  request from looking like a demand spike.

- **"What would you change at 100x?"** Shard by region (keys are already
  region-scoped), switch surge to H3 cells with incremental counters,
  and move from greedy to batched matching, which trades a small delay
  for better global assignment. I can explain why greedy nearest is
  locally optimal and globally suboptimal.

- **"What is explicitly not done?"** Real routing, payments, pooled
  rides, multi-region deployment. All are listed with reasons in the
  PRD and architecture docs, not discovered as gaps.