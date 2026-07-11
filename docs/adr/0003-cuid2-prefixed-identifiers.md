# ADR-0003: CUID2 identifiers with type prefixes

Date: 2026-07-10 · Status: accepted (developer decision at discovery review)

## Context
Composio's dual-id system (UUID vs nano id + translation API) and Membrane's
reconnect-changes-the-id behavior are two of the worst vendor pain points Beecon
exists to fix.

## Decision
- Hard rule: **exactly one immutable id per entity, forever** — ids never change,
  including Connection ids across re-auth. Never a second id system.
- Format: type-prefixed CUID2 — `org_`, `user_`, `key_`, `conn_`, `tool_`, `trg_` +
  CUID2 body. Generator: `github.com/akshayvadher/cuid2` (Go port of
  paralleldrive/cuid2; SHA3-512, collision-resistant, URL-safe base36).
- Accepted trade-off: CUID2 is deliberately not time-sortable (no B-tree insert
  locality); order by `created_at` instead. Negligible at self-hosted volume.

## Consequences
- Ids are self-describing in logs; cross-entity mix-ups visible at a glance.
- `internal/idgen.Prefixed(prefix)` mints per-entity `func() string` minters, injected
  into facades (deterministic `sequentialIDs` fakes in tests).
