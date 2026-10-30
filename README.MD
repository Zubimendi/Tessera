# Tessera

A ride and delivery dispatch engine in Go, built around the question
every marketplace has to answer correctly: **who gets this ride, and
what does it cost?**

**Status: design phase.** This repository currently holds architecture
and build specifications only. See `docs/CURSOR_CONTEXT.md` for the
build plan. The sections below describe what the finished system is
designed to do, not what already runs.

## What makes this different from "find the nearest driver"

Most dispatch demos query a table ordered by distance and call it done.
Tessera treats the hard parts as first-class:

- **No double assignment.** Claiming a driver is an atomic removal from
  a Redis geo set, backed by Postgres partial unique indexes that refuse
  a second active assignment even if Redis is wrong.
- **Pricing you can explain.** Surge is an ordered set of declarative,
  versioned rules evaluated by a pure function. Money is integer minor
  units and multipliers are integer basis points. Every quote records
  which rules fired.
- **An explicit state machine.** Trip transitions are enforced in Go,
  by compare-and-swap writes, and by a database trigger, with an
  append-only event log of every change.
- **Live tracking that degrades gracefully.** Stateless WebSocket
  gateways, latest-wins location delivery, bounded buffers.
- **Redis is allowed to be wrong.** A reconciler checks assignment state
  against Postgres on a schedule and records every run.

## Designed features

- Geospatial candidate search with expanding rings and ETA ranking
  behind a replaceable interface
- Sequential offers with durable deadlines that survive crashes
- Dynamic pricing with surge tiers, smoothing, caps, and a thin-market
  guard
- Locked quotes (the price shown is the price charged)
- Idempotent ride creation
- Driver and rider WebSockets with heartbeats, rate limits, and
  plausibility checks
- Scheduled surge calculation and scheduled reconciliation
- Driver simulator for demos and load testing

## Quickstart (planned, not yet working)

Requires Go 1.22+, Docker, Docker Compose.

```bash
cp .env.example .env
make up          # Postgres + PostGIS, Redis, migrations
make api         # REST + WebSocket gateway
make matcher     # offer loop workers
make surge       # scheduled surge calculator
make reconciler  # scheduled invariant checks
make sim         # N simulated drivers, M simulated riders
```

## Documentation

- [`docs/PRD.md`](docs/PRD.md): problem, users, scope
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): every design decision,
  its mechanism, and its trade-off
- [`docs/TESTING.md`](docs/TESTING.md): test plan and manual
  demonstrations
- [`docs/STORY.md`](docs/STORY.md): narrative and interview talking
  points
- [`docs/CURSOR_CONTEXT.md`](docs/CURSOR_CONTEXT.md): build spec, phases,
  and decisions

## Repo layout

```
cmd/            api, matcher, surge, reconciler, driversim
internal/       geo, dispatch, pricing, eta, ws, store, auth, config, obs
migrations/     schema, triggers, indexes
postman/        collection and environment
docs/
```

## License

MIT, see `LICENSE`.