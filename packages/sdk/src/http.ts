import { BeeconApiError, RateLimitedError } from './errors.js';

export type FetchLike = typeof fetch;

export interface HttpClientConfig {
  baseUrl: string;
  apiKey: string;
  fetchImpl?: FetchLike;
}

type QueryParams = Record<string, string | number | boolean | undefined>;

// The Node "nodejs.util.inspect.custom" well-known symbol, obtained via
// Symbol.for so the SDK never has to import the 'node:util' module (keeping
// it usable outside Node too). console.log(client) / util.inspect(client)
// both honor it.
const inspectSymbol = Symbol.for('nodejs.util.inspect.custom');

// HttpClient is the SDK's only network boundary. The API key lives in a
// private class field — it is never assigned to an enumerable property, so
// it cannot appear in JSON.stringify(client), console.log(client), or any
// error thrown from this class (AC9). fetchImpl is injectable so tests can
// stub the network without a live server.
export class HttpClient {
  readonly #apiKey: string;
  readonly #baseUrl: string;
  readonly #fetchImpl: FetchLike;

  constructor(config: HttpClientConfig) {
    this.#apiKey = config.apiKey;
    this.#baseUrl = config.baseUrl.replace(/\/+$/, '');
    this.#fetchImpl = config.fetchImpl ?? globalThis.fetch;
  }

  get<T>(path: string, query?: QueryParams): Promise<T> {
    return this.send<T>('GET', path, undefined, query);
  }

  post<T>(path: string, body?: unknown): Promise<T> {
    return this.send<T>('POST', path, body);
  }

  delete<T>(path: string): Promise<T> {
    return this.send<T>('DELETE', path);
  }

  // postMultipart sends a multipart/form-data POST (files.upload's only
  // caller today): fetch derives the correct Content-Type (with boundary)
  // from the FormData body itself, so buildHeaders' JSON content type is
  // deliberately not used here.
  async postMultipart<T>(path: string, form: FormData): Promise<T> {
    const response = await this.#fetchImpl(this.buildUrl(path), {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.#apiKey}` },
      body: form,
    });
    return handleResponse<T>(response);
  }

  toJSON(): { baseUrl: string } {
    return { baseUrl: this.#baseUrl };
  }

  [inspectSymbol](): string {
    return `HttpClient { baseUrl: ${JSON.stringify(this.#baseUrl)} }`;
  }

  private async send<T>(
    method: string,
    path: string,
    body?: unknown,
    query?: QueryParams,
  ): Promise<T> {
    const response = await this.#fetchImpl(this.buildUrl(path, query), {
      method,
      headers: this.buildHeaders(),
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return handleResponse<T>(response);
  }

  private buildUrl(path: string, query?: QueryParams): string {
    const url = new URL(`${this.#baseUrl}${path}`);
    for (const [key, value] of Object.entries(query ?? {})) {
      if (value !== undefined) {
        url.searchParams.set(key, String(value));
      }
    }
    return url.toString();
  }

  private buildHeaders(): HeadersInit {
    return {
      Authorization: `Bearer ${this.#apiKey}`,
      'Content-Type': 'application/json',
    };
  }
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (response.status === 204) {
    return undefined as T;
  }
  const text = await response.text();
  const parsed = text ? parseJsonSafely(text) : undefined;
  if (!response.ok) {
    throw toApiError(response.status, parsed, response.headers.get('Retry-After'));
  }
  return parsed as T;
}

function parseJsonSafely(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

// toApiError builds the platform-level error for a non-2xx response. A 429
// is PD21's deliberate carve-out: it always becomes a RateLimitedError
// (never a plain BeeconApiError) carrying retryAfter parsed from the
// Retry-After header, in whole seconds.
function toApiError(status: number, body: unknown, retryAfterHeader: string | null): BeeconApiError {
  const envelope = isErrorEnvelope(body) ? body.error : undefined;
  const errorBody = {
    code: envelope?.code ?? 'unknown_error',
    message: envelope?.message ?? `Request failed with status ${status}`,
    details: envelope?.details,
  };
  if (status === 429) {
    return new RateLimitedError(parseRetryAfter(retryAfterHeader), errorBody);
  }
  return new BeeconApiError(status, errorBody);
}

function parseRetryAfter(header: string | null): number {
  const seconds = Number(header);
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : 0;
}

function isErrorEnvelope(
  body: unknown,
): body is { error: { code: string; message: string; details?: Record<string, unknown> } } {
  return typeof body === 'object' && body !== null && 'error' in body;
}
