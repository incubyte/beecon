// Shared response-building helpers for HttpClient/resource tests. Building
// real `Response` instances (rather than hand-rolled fakes) exercises the
// exact `.ok`/`.status`/`.text()` contract handleResponse() relies on.
import type { FetchLike } from '../../src/http.js';

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  });
}

export function rawResponse(body: string, status: number): Response {
  return new Response(body, { status });
}

export function noContentResponse(): Response {
  return new Response(null, { status: 204 });
}

// vi.fn()'s inferred mock type doesn't structurally match the overloaded
// `typeof fetch` signature; this narrows the cast to one place.
export function asFetch(mock: unknown): FetchLike {
  return mock as FetchLike;
}
