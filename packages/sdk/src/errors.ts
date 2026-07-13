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
