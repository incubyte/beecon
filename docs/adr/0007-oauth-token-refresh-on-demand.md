# ADR-0007: OAuth token refresh — on-demand at execution (Phase 2)

Date: 2026-07-12 · Status: accepted (spec PD18 — developer-confirmed)

## Context
Phase 1 stored refresh tokens (scope `offline_access`) but never used them: access
tokens die after ~1 hour and the connection silently stops working. The discovery
milestone map placed "auto token refresh" in Phase 3 (background reconciliation), but
without any refresh in Phase 2, the EXPIRED status is unreachable and every demo
connection dies within the hour.

## Decision
Phase 2 refreshes tokens **on demand, in the execution path only**: when a tool call
finds the stored access token expired (or the provider rejects it as expired), the
platform exchanges the refresh token, re-encrypts and stores the new pair (handling
rotated refresh tokens), and retries the call once. A refresh failure marks the
connection EXPIRED. The proactive background refresh scheduler remains Phase 3.

## Consequences
- Connections stay usable indefinitely under active use; EXPIRED becomes a real,
  testable state reached only when refresh actually fails.
- No scheduler, queue, or extra runtime enters Phase 2 (thin-wrapper ethos holds).
- Rarely-used connections may still present first-call latency (refresh happens
  inline); Phase 3's scheduler removes that.
