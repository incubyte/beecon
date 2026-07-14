import { http, HttpResponse } from "msw";

/**
 * Default MSW handlers every test starts with (§2.9 of the architecture
 * doc): realistic PD5-shaped responses for the two endpoints Slice 1 talks
 * to, so a test that doesn't care about a particular request still gets a
 * plausible response instead of an MSW "unhandled request" error.
 *
 * Defaults are deliberately the "closed" / "empty" case — `/admin/verify`
 * rejects by default, and the organizations list is empty by default — so a
 * test opts in to the interesting case (`server.use(...)`) rather than a
 * silently-permissive default masking a missing assertion.
 */
export const handlers = [
  http.get("/admin/verify", () => new HttpResponse(null, { status: 401 })),
  http.get("/api/v1/organizations", () => HttpResponse.json({ items: [] })),
  // Slice 3 defaults: empty logs/events pages and a zeroed dashboard summary
  // so a test that doesn't care about these endpoints still gets a
  // plausible response instead of an MSW "unhandled request" error.
  http.get("/api/v1/organizations/:orgId/logs", () => HttpResponse.json({ entries: [] })),
  http.get("/api/v1/organizations/:orgId/events", () => HttpResponse.json({ items: [] })),
  http.get("/api/v1/dashboard/metrics", () =>
    HttpResponse.json({
      connectionsByStatus: {},
      outbox: { pendingDepth: 0, oldestPendingAgeSeconds: 0 },
      deliveryOutcomes: [],
    }),
  ),
];
