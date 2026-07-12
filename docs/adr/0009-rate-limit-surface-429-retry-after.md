# ADR-0009: Rate-limit surface — HTTP 429 + Retry-After carve-out

Date: 2026-07-12 · Status: accepted (spec PD21 — developer-confirmed)

## Context
Phase 2 normalizes provider rate limits (Graph, Hubspot) and retries platform-side,
honoring Retry-After. When retries are exhausted, the failure must surface to the
consumer. PD6 (Phase 1) routes tool-level failures through HTTP 200
`{successful:false, error, data:null}`; discovery called for "one retriable shape"
across providers.

## Decision
Rate-limit exhaustion is a deliberate carve-out from the PD6 envelope: the API returns
**HTTP 429 with a Retry-After header** (and a `rate_limited` error envelope body).
The SDK surfaces this as a typed `RateLimitedError` carrying `retryAfter`. All other
tool-level failures stay inside the 200 envelope.

## Consequences
- Consumers get standard HTTP semantics — generic HTTP clients, proxies, and rolai's
  retry logic can react to 429 without parsing Beecon-specific bodies.
- The PD6 rule gains exactly one exception; it is documented here and in the spec, and
  the SDK type system makes the split explicit (thrown `RateLimitedError` vs returned
  envelope).
