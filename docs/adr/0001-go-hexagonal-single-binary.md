# ADR-0001: Go hexagonal modular monolith, shipped as a single binary

Date: 2026-07-10 · Status: accepted (developer-approved at architecture review)

## Context
Beecon must stay small (thin wrapper over provider APIs + token-refresh job) and be
trivially self-hostable by each installation (Rolai, eCW). The team's reference
implementation of Go hexagonal style is `rolai-university/server-go`. Discovery left
"single deployment vs split UI/backend" open.

## Decision
- Go backend, hexagonal modules mirroring `rolai-university` exactly: `types/port/
  facade/errors` + `driven/bun`, `driven/memory`, `driving/httpapi` per module.
- PocketBase-style **single binary** (`beecon serve`): API + Go-template connect pages
  + embedded migrations + embedded provider definitions. Phase 4 admin UI (TanStack
  Start) embeds as a static build in the same binary.
- TypeScript SDK lives in `packages/sdk`, separate from the Go module.

## Consequences
- One artifact, one config surface; lowest possible self-hosting friction.
- Split into separate deployables only on a real trigger: UI release cadence diverging
  from API, CDN/edge requirements, or independent worker scaling (Phase 3+).
