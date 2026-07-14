import { clearAdminKey, getAdminKey } from "./auth";
import type { DomainErrorBody } from "./api-types";

/** ApiError is the typed shape every rejected api-client call throws,
 * carrying the PD5 envelope's machine-readable code and any details. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details?: Record<string, unknown>;

  constructor(status: number, body: DomainErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code;
    this.details = body.details;
  }
}

export interface ApiClientOptions {
  /** Injected so tests (MSW) and callers never depend on the global fetch
   * directly — the boundary the slice-tester needs (design brief §"Boundaries
   * are explicit"). */
  fetchImpl?: typeof fetch;
  getKey?: () => string | null;
  onUnauthorized?: () => void;
}

export interface ApiClient {
  get<T>(path: string): Promise<T>;
  post<T>(path: string, body?: unknown): Promise<T>;
  put<T>(path: string, body?: unknown): Promise<T>;
  delete<T>(path: string): Promise<T>;
}

/**
 * createApiClient builds a thin fetch wrapper over /api/v1: it injects
 * `Authorization: Bearer <admin key>` from the in-memory PD39 store on
 * every call, parses the PD5 DomainError envelope into a typed ApiError,
 * and — on a 401, whether the key was wrong or an existing session expired
 * mid-use — clears the in-memory key so the gate guard in routes/__root.tsx
 * takes over on the next render (Slice 1, AC7). Every dependency is a
 * constructor parameter, never a hardcoded import, so a test can substitute
 * its own fetch and its own key store.
 */
export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const getKey = options.getKey ?? getAdminKey;
  const onUnauthorized = options.onUnauthorized ?? clearAdminKey;

  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    // Resolved per-request, not captured once at createApiClient() call time:
    // the module-level `apiClient` singleton below is constructed the moment
    // this module is first imported, which — under a test runner that
    // installs a network mock (MSW) via a lifecycle hook rather than at
    // import time — can run before that mock has patched the global fetch.
    // A per-request lookup always sees whatever `fetch` currently resolves
    // to; a one-time capture at construction would permanently pin the
    // pre-mock reference for the app's one real singleton instance.
    const doFetch = options.fetchImpl ?? fetch;
    const headers = new Headers(init?.headers);
    headers.set("Accept", "application/json");
    const key = getKey();
    if (key) {
      headers.set("Authorization", `Bearer ${key}`);
    }

    const response = await doFetch(`/api/v1${path}`, { ...init, headers });

    if (response.status === 401) {
      onUnauthorized();
    }
    if (!response.ok) {
      throw new ApiError(response.status, await parseErrorBody(response));
    }
    return parseSuccessBody<T>(response);
  }

  return {
    get: (path) => request(path, { method: "GET" }),
    post: (path, body) =>
      request(path, {
        method: "POST",
        headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      }),
    put: (path, body) =>
      request(path, {
        method: "PUT",
        headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
      }),
    delete: (path) => request(path, { method: "DELETE" }),
  };
}

/**
 * parseSuccessBody resolves a 2xx response's body: an empty body — 204 No
 * Content, 205, or any other success status the backend answers with zero
 * bytes (e.g. the redeliver endpoint's 202 Accepted, confirmed empty by the
 * Go integration test) — resolves to undefined rather than calling
 * response.json() on an empty string, which throws "Unexpected end of JSON
 * input". Reading text() first (instead of keying off a fixed status list)
 * makes this robust to any current or future empty-bodied success status,
 * not just the ones already known about.
 */
async function parseSuccessBody<T>(response: Response): Promise<T> {
  const text = await response.text();
  if (text === "") {
    return undefined as T;
  }
  return JSON.parse(text) as T;
}

async function parseErrorBody(response: Response): Promise<DomainErrorBody> {
  try {
    const parsed = (await response.json()) as { error?: DomainErrorBody };
    if (parsed.error) {
      return parsed.error;
    }
  } catch {
    // Not a JSON body (or no body at all) — fall through to the generic
    // shape below rather than throwing a parse error out of an error path.
  }
  return { code: "unknown_error", message: response.statusText || "Request failed" };
}

/** The app's single, real api-client instance. Tests build their own via
 * createApiClient with a mocked fetchImpl instead of importing this. */
export const apiClient = createApiClient();

/**
 * verifyAdminKey calls GET /admin/verify (FD3) with the given candidate key
 * — never the in-memory stored one, since this runs before the gate has
 * accepted it — and reports whether the key is valid (204) or not (401 or
 * any other failure). It never throws: the gate screen shows the same
 * inline error either way (Slice 1, AC5).
 */
export async function verifyAdminKey(key: string, fetchImpl: typeof fetch = fetch): Promise<boolean> {
  try {
    const response = await fetchImpl("/admin/verify", {
      headers: { Authorization: `Bearer ${key}` },
    });
    return response.status === 204;
  } catch {
    return false;
  }
}
