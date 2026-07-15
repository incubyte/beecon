import { readCsrfToken } from "./csrf";
import { markSessionExpiredMidWork } from "./session-state";
import type { DomainErrorBody } from "./api-types";

/** mutatingMethods is the double-submit CSRF header's own scope (Slice 3,
 * PD52): the session cookie's X-CSRF-Token check on the server applies only
 * to these — GET/HEAD never carry (or need) the header. */
const mutatingMethods = new Set(["POST", "PUT", "PATCH", "DELETE"]);

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
  onUnauthorized?: () => void;
}

export interface ApiClient {
  get<T>(path: string): Promise<T>;
  post<T>(path: string, body?: unknown): Promise<T>;
  put<T>(path: string, body?: unknown): Promise<T>;
  delete<T>(path: string): Promise<T>;
}

/**
 * createApiClient builds a thin fetch wrapper over /api/v1 (Phase 5 Slice 1,
 * PD49/PD55): it sends the same-origin `beecon_session` cookie automatically
 * (browsers attach same-origin cookies to `fetch` by default — no JS-held
 * credential of any kind, unlike the retired PD39 admin-key store), parses
 * the PD5 DomainError envelope into a typed ApiError, and — on a 401 from
 * any call *other than* the session probe itself — flags the session as
 * expired mid-work (Slice 5, `markSessionExpiredMidWork`) rather than
 * invalidating the `auth.me` probe directly: the cached authenticated probe
 * result is left in place, so AppShell stays mounted underneath ReauthModal
 * instead of a hard bounce to LoginScreen that would lose in-progress page
 * state. (The probe's own 401 is already the query's normal "not
 * authenticated" error state — LoginScreen and the initial unauthenticated
 * load are untouched by this.) Every dependency is a constructor
 * parameter, never a hardcoded import, so a test can substitute its own
 * fetch and its own onUnauthorized.
 *
 * On every mutating request (POST/PUT/PATCH/DELETE) it also attaches
 * `X-CSRF-Token`, read fresh from the non-HttpOnly beecon_csrf cookie via
 * readCsrfToken() on each call (Slice 3, PD52) — the double-submit value the
 * server's authmw.ConsoleAuth/OperatorSession compare against the session's
 * own CSRF token. GET/HEAD never attach it; when the cookie is absent (no
 * session yet, e.g. the login call itself) the header is simply omitted, and
 * the server rejects the request on the missing session rather than a CSRF
 * mismatch.
 */
export function createApiClient(options: ApiClientOptions = {}): ApiClient {
  const onUnauthorized = options.onUnauthorized ?? markSessionExpiredMidWork;

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
    if (init?.method && mutatingMethods.has(init.method)) {
      const csrfToken = readCsrfToken();
      if (csrfToken) {
        headers.set("X-CSRF-Token", csrfToken);
      }
    }

    const response = await doFetch(`/api/v1${path}`, {
      ...init,
      headers,
      // Explicit, even though "same-origin" is fetch's own default: the
      // whole PD52 session model depends on the beecon_session cookie
      // riding every /api/v1 call the SPA makes under /admin, same-origin.
      credentials: "same-origin",
    });

    if (response.status === 401 && path !== "/auth/me") {
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
