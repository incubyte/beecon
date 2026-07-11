# ADR-0002: Postgres + SQLite behind one persistence port, one bun adapter

Date: 2026-07-10 · Status: accepted (developer-approved at architecture review)

## Context
Requirements say "Postgres, but configurable" (hexagonal). Developer added SQLite for
local development. Two hand-written SQL adapters would double the security-critical
query surface (org isolation lives in WHERE clauses).

## Decision
- ONE `driven/bun` repository per module, running on both engines: `pgdriver` +
  `pgdialect` (production) and `modernc.org/sqlite` + `sqlitedialect` (local/tests —
  pure Go, no cgo).
- Port swappability additionally proven by `driven/memory` fakes used in unit tests.
- Migrations: `uptrace/bun/migrate`, embedded `.up.sql`/`.down.sql` files, run at boot.
- Dialect quirks (unique-violation detection, timestamp defaults) handled in shared
  helpers inside the adapter.

## Consequences
- Org-isolation SQL is written once. Tests run on SQLite in-memory without Docker;
  one Postgres-parity journey runs behind build tag `integration_pg` (testcontainers).
- A genuinely different store (Mongo) becomes a real second adapter only when actually
  scheduled.
