# ADR-0011: File uploads — retention deferred to Phase 4

Date: 2026-07-12 · Status: accepted (spec PD22 — developer-confirmed)

## Context
Phase 2 adds file upload for file-typed tool inputs (`file_` ids + download URLs,
proven via `hubspot-upload-file`). Stored files raise a retention question.

## Decision
Phase 2 ships upload + download with **no expiry or retention policy** — files persist
until explicitly deleted. Retention/expiry configuration lands in Phase 4 alongside log
retention, as part of the governance surface.

## Consequences
- Phase 2 stays small; no TTL machinery or cleanup jobs.
- Operators of long-running Phase 2/3 installations must be aware storage grows
  unbounded until Phase 4 — acceptable for the rolai-internal adoption window.
