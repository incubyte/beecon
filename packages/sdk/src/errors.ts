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
