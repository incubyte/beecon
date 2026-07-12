# ADR-0008: User-scoped browser tokens — HS256 with vault-encrypted org signing secrets

Date: 2026-07-12 · Status: accepted (spec PD20 — developer-confirmed)

## Context
Phase 2 gives the browser a limited surface (list integrations, initiate, own
connections, reconnect) authenticated by short-lived (~2h) user-scoped tokens, minted
locally by the consumer's server via the SDK — no Beecon round-trip per token, mirroring
Membrane's ergonomics. Symmetric HS256 requires the verifying side (Beecon) to hold the
same secret the consumer signs with, so the secret must be stored recoverably —
encrypted, not hashed, unlike API keys (ADR-0005).

## Decision
Per-organization signing secrets, stored AES-256-GCM encrypted in the existing vault
pattern (same as OAuth tokens). The SDK signs HS256 tokens locally with the raw secret
the admin fetched once at creation; Beecon decrypts its copy to verify. Asymmetric
signing (Ed25519/RS256) was considered and rejected for Phase 2: more moving parts
(key pairs, JWKS-style distribution) without a present need, since both signer and
verifier are trusted parties.

## Consequences
- One new recoverable-secret class in the vault; compromise of a signing secret forges
  that org's browser tokens until the secret is rotated — rotation must exist from day
  one.
- Token minting is a pure local operation in the SDK (no latency, works offline).
- If third parties ever need to verify tokens (not currently planned), revisit with
  asymmetric keys.
