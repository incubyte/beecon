import { BeeconApiError } from './errors.js';

export type FetchLike = typeof fetch;

export interface HttpClientConfig {
  baseUrl: string;
  apiKey: string;
  fetchImpl?: FetchLike;
}

type QueryParams = Record<string, string | number | undefined>;

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
    throw toApiError(response.status, parsed);
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

function toApiError(status: number, body: unknown): BeeconApiError {
  const envelope = isErrorEnvelope(body) ? body.error : undefined;
  return new BeeconApiError(status, {
    code: envelope?.code ?? 'unknown_error',
    message: envelope?.message ?? `Request failed with status ${status}`,
    details: envelope?.details,
  });
}

function isErrorEnvelope(
  body: unknown,
): body is { error: { code: string; message: string; details?: Record<string, unknown> } } {
  return typeof body === 'object' && body !== null && 'error' in body;
}
