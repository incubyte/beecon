export interface ApiErrorBody {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

// BeeconApiError is thrown only for platform-level HTTP failures (PD5):
// unauthorized, not_found, validation_failed, and the like. It carries the
// API's own machine-readable code and message untouched — never the request
// body, headers, or the API key that authenticated the call (AC9).
export class BeeconApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = 'BeeconApiError';
    this.status = status;
    this.code = body.code;
    this.details = body.details;
    Object.setPrototypeOf(this, BeeconApiError.prototype);
  }
}

// RateLimitedError is PD21's typed carve-out: an exhausted platform-side
// retry surfaces as HTTP 429 with a Retry-After header, and the SDK raises
// this subclass (rather than a plain BeeconApiError) so callers can branch
// on `instanceof RateLimitedError` and read retryAfter directly, while still
// catching it with a plain `instanceof BeeconApiError` handler if they want
// one code path for every platform-level failure.
export class RateLimitedError extends BeeconApiError {
  /** Seconds to wait before retrying, parsed from the Retry-After header. */
  readonly retryAfter: number;

  constructor(retryAfter: number, body: ApiErrorBody) {
    super(429, body);
    this.name = 'RateLimitedError';
    this.retryAfter = retryAfter;
    Object.setPrototypeOf(this, RateLimitedError.prototype);
  }
}

// MissingSigningSecretError is thrown by userTokens.create when the Beecon
// client was constructed without a signingSecret (PD20) — minting a user
// token is a purely local operation, so there is no server round-trip that
// could otherwise surface this as a BeeconApiError.
export class MissingSigningSecretError extends Error {
  constructor() {
    super(
      'beecon.userTokens.create requires a signing secret: construct Beecon with signingSecret: { id, secret }.',
    );
    this.name = 'MissingSigningSecretError';
    Object.setPrototypeOf(this, MissingSigningSecretError.prototype);
  }
}

// UserTokenExpiryTooLongError is thrown by userTokens.create when the
// requested expiresIn exceeds the 24-hour maximum lifetime (PD38a, Phase 2
// review carry-forward): the server's VerifyUserToken rejects exp − iat
// beyond that span anyway, so the SDK refuses to mint one at all rather than
// handing back a token that can never verify.
export class UserTokenExpiryTooLongError extends Error {
  constructor(requestedExpiresIn: number, maxExpiresIn: number) {
    super(
      `beecon.userTokens.create: expiresIn (${requestedExpiresIn}s) exceeds the maximum user token lifetime of ${maxExpiresIn}s (24h).`,
    );
    this.name = 'UserTokenExpiryTooLongError';
    Object.setPrototypeOf(this, UserTokenExpiryTooLongError.prototype);
  }
}

// WebhookVerificationReason names why webhooks.verify rejected a delivery
// (PD27): malformed-header (one of the three Standard Webhooks headers is
// missing, empty, or unparseable), signature (the webhook-signature header
// carries no recognized "v1," entry), tampered (a v1 entry is present but no
// provided secret's HMAC matches it -- the payload was altered after
// signing, or the wrong secret was supplied; the two are cryptographically
// indistinguishable from the verifier's side, so they share this reason),
// and timestamp (webhook-timestamp is outside the +/-5-minute tolerance).
export type WebhookVerificationReason = 'malformed-header' | 'signature' | 'tampered' | 'timestamp';

// WebhookVerificationError is thrown by webhooks.verify. It carries only a
// typed reason and a human message -- never the secret(s) it was given to
// verify with, never the raw signature bytes -- so catching and logging
// this error (stack, message, JSON.stringify) can never leak a whsec_ value
// (parity with the API-key/signing-secret guarantee).
export class WebhookVerificationError extends Error {
  readonly reason: WebhookVerificationReason;

  constructor(reason: WebhookVerificationReason, message: string) {
    super(message);
    this.name = 'WebhookVerificationError';
    this.reason = reason;
    Object.setPrototypeOf(this, WebhookVerificationError.prototype);
  }
}
