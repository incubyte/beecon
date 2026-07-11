# ADR-0005: Organization API key scheme

Date: 2026-07-10 · Status: accepted (spec PD1/PD3 + slice 2 implementation)

## Context
Membrane uses per-user JWTs; Composio one global key. Beecon needs org-scoped server
keys now and short-lived user tokens later (Phase 2), with secrets that are safe in a
database dump.

## Decision
- Secret format `beecon_sk_<random>` (crypto/rand, 32 bytes, base64url). Entity id is
  separate (`key_<cuid2>`).
- Storage: 12-char plaintext lookup prefix + SHA-256 hash of the remainder. Full
  secret returned exactly once, at issue. Constant-time hash comparison
  (crypto/subtle).
- The 12-char lookup prefix contains only ~2 random chars, so prefix collisions are
  expected: `PrefixLookup.FindByPrefix` returns ALL candidates and `Verify`
  disambiguates by hash (rejects if none match). This lookup is deliberately
  installation-level (verification happens before an org is known) — a separate,
  whitelisted interface.
- SHA-256 (not bcrypt/argon2) is correct here: the secret is high-entropy random, not
  a human password; KDF stretching adds latency without security benefit.
- Phase 1 ships issue + immediate revoke; rotation-with-overlap deferred (Phase 2/4).
- Installation admin operations authenticate with env-configured
  `BEECON_ADMIN_API_KEY` (no admin-user entity until Phase 4).

## Consequences
- A DB dump exposes no usable secret. Keys are recognizable in config by prefix.
- Consumers (rolai) hold one org key server-side, mirroring the Composio ergonomics
  they already have, but org-scoped.
