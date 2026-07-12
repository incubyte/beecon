# ADR-0010: API key rotation — overlap window, 24h default

Date: 2026-07-12 · Status: accepted (spec PD23 — developer-confirmed)

## Context
ADR-0005 shipped issue + immediate revoke only. Rotating a key with zero overlap
forces a coordinated config deploy; rolai needs to roll new secrets without downtime.

## Decision
Rotation mints a new secret while the old one stays valid for an overlap window —
**24 hours by default** (covers a full deploy cycle with margin). Revoke remains
immediate and kills both secrets. Storage/verification follow ADR-0005 unchanged
(prefix + SHA-256 hash per active secret).

## Consequences
- Zero-downtime secret rolls for consumers; at most two secrets are live per key
  during the window.
- The 24h default is a constant in Phase 2; making it caller-configurable is a small
  later extension if deploy cadence demands it.
