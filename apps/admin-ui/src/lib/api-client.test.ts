import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";

import { ApiError, apiClient, createApiClient } from "./api-client";
import { queryClient, queryKeys } from "./query";

afterEach(() => {
  document.cookie = "beecon_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/";
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
});

describe("createApiClient", () => {
  it("sends no Authorization header — the session cookie authenticates instead", async () => {
    let capturedAuthHeader: string | null = "not-yet-captured";
    server.use(
      http.get("/api/v1/widgets", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return HttpResponse.json({ items: [] });
      }),
    );
    const client = createApiClient();

    await client.get("/widgets");

    expect(capturedAuthHeader).toBeNull();
  });

  it("resolves GET with the parsed JSON body", async () => {
    server.use(http.get("/api/v1/widgets", () => HttpResponse.json({ items: ["a", "b"] })));
    const client = createApiClient();

    const result = await client.get<{ items: string[] }>("/widgets");

    expect(result.items).toEqual(["a", "b"]);
  });

  it("sends a JSON body on POST", async () => {
    let capturedBody: unknown;
    server.use(
      http.post("/api/v1/widgets", async ({ request }) => {
        capturedBody = await request.json();
        return HttpResponse.json({ id: "widget_1" }, { status: 201 });
      }),
    );
    const client = createApiClient();

    await client.post("/widgets", { name: "Acme" });

    expect(capturedBody).toEqual({ name: "Acme" });
  });

  // --- 401 handling (Phase 5 Slice 1, PD49/PD55: a call other than the
  // session probe itself returning 401 invalidates the auth.me probe so the
  // SPA falls back to the login screen). ---

  it("calls onUnauthorized when a request other than /auth/me returns 401", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 401 })));
    let unauthorizedCalls = 0;
    const client = createApiClient({ onUnauthorized: () => unauthorizedCalls++ });

    await client.get("/widgets").catch(() => {});

    expect(unauthorizedCalls).toBe(1);
  });

  it("does not call onUnauthorized for the /auth/me probe's own 401 (its own query error state already conveys this)", async () => {
    server.use(http.get("/api/v1/auth/me", () => new HttpResponse(null, { status: 401 })));
    let unauthorizedCalls = 0;
    const client = createApiClient({ onUnauthorized: () => unauthorizedCalls++ });

    await client.get("/auth/me").catch(() => {});

    expect(unauthorizedCalls).toBe(0);
  });

  it("does not call onUnauthorized for a non-401 error", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 500 })));
    let unauthorizedCalls = 0;
    const client = createApiClient({ onUnauthorized: () => unauthorizedCalls++ });

    await client.get("/widgets").catch(() => {});

    expect(unauthorizedCalls).toBe(0);
  });

  // --- The real default onUnauthorized (Slice 5): markSessionExpiredMidWork,
  // not a hard session invalidation — flags the reauthRequired flag rather
  // than tearing down the cached auth.me probe, so AppShell stays mounted
  // underneath ReauthModal instead of bouncing to LoginScreen. Exercised
  // through the module's real singleton `apiClient` (not a custom-built
  // client with an injected callback), since the default itself is what's
  // under test here. ---
  it("the default client (no onUnauthorized override) flags the session expired mid-work on a 401 from a call other than /auth/me", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 401 })));

    await apiClient.get("/widgets").catch(() => {});

    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(true);
  });

  it("the default client does not flag the session expired mid-work for the /auth/me probe's own 401", async () => {
    server.use(http.get("/api/v1/auth/me", () => new HttpResponse(null, { status: 401 })));

    await apiClient.get("/auth/me").catch(() => {});

    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false);
  });

  // --- PD5 DomainError envelope parsing. ---

  it("throws a typed ApiError with the PD5 envelope's code, message, and details for a non-2xx response", async () => {
    server.use(
      http.get("/api/v1/widgets", () =>
        HttpResponse.json(
          { error: { code: "validation_failed", message: "validation failed", details: { field: "name" } } },
          { status: 422 },
        ),
      ),
    );
    const client = createApiClient();

    await expect(client.get("/widgets")).rejects.toMatchObject({
      status: 422,
      code: "validation_failed",
      message: "validation failed",
      details: { field: "name" },
    });
  });

  it("throws an ApiError instance specifically, not a plain object", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 500 })));
    const client = createApiClient();

    await expect(client.get("/widgets")).rejects.toBeInstanceOf(ApiError);
  });

  it("falls back to a generic error shape when the error body isn't valid JSON", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse("not json", { status: 500 })));
    const client = createApiClient();

    await expect(client.get("/widgets")).rejects.toMatchObject({ status: 500, code: "unknown_error" });
  });

  it("returns undefined for a 204 No Content response", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 204 })));
    const client = createApiClient();

    const result = await client.get("/widgets");

    expect(result).toBeUndefined();
  });

  it("resolves (does not throw) for a 202 Accepted response with an empty body", async () => {
    server.use(http.post("/api/v1/widgets", () => new HttpResponse(null, { status: 202 })));
    const client = createApiClient();

    await expect(client.post("/widgets")).resolves.toBeUndefined();
  });

  // --- X-CSRF-Token double-submit header (Phase 5 Slice 3, PD52): attached
  // automatically on every mutating call, read fresh from the non-HttpOnly
  // beecon_csrf cookie readCsrfToken() exposes; omitted on GET, where the
  // server never checks it. ---

  describe("X-CSRF-Token header", () => {
    it("attaches X-CSRF-Token (read from the beecon_csrf cookie) on POST", async () => {
      document.cookie = "beecon_csrf=the-csrf-token";
      let capturedHeader: string | null = "not-yet-captured";
      server.use(
        http.post("/api/v1/widgets", ({ request }) => {
          capturedHeader = request.headers.get("X-CSRF-Token");
          return HttpResponse.json({ id: "widget_1" }, { status: 201 });
        }),
      );
      const client = createApiClient();

      await client.post("/widgets", { name: "Acme" });

      expect(capturedHeader).toBe("the-csrf-token");
    });

    it("attaches X-CSRF-Token on PUT", async () => {
      document.cookie = "beecon_csrf=the-csrf-token";
      let capturedHeader: string | null = "not-yet-captured";
      server.use(
        http.put("/api/v1/widgets/widget_1", ({ request }) => {
          capturedHeader = request.headers.get("X-CSRF-Token");
          return HttpResponse.json({ id: "widget_1" });
        }),
      );
      const client = createApiClient();

      await client.put("/widgets/widget_1", { name: "Acme" });

      expect(capturedHeader).toBe("the-csrf-token");
    });

    it("attaches X-CSRF-Token on DELETE", async () => {
      document.cookie = "beecon_csrf=the-csrf-token";
      let capturedHeader: string | null = "not-yet-captured";
      server.use(
        http.delete("/api/v1/widgets/widget_1", ({ request }) => {
          capturedHeader = request.headers.get("X-CSRF-Token");
          return new HttpResponse(null, { status: 204 });
        }),
      );
      const client = createApiClient();

      await client.delete("/widgets/widget_1");

      expect(capturedHeader).toBe("the-csrf-token");
    });

    it("omits X-CSRF-Token on GET", async () => {
      document.cookie = "beecon_csrf=the-csrf-token";
      let capturedHeader: string | null = "not-yet-captured";
      server.use(
        http.get("/api/v1/widgets", ({ request }) => {
          capturedHeader = request.headers.get("X-CSRF-Token");
          return HttpResponse.json({ items: [] });
        }),
      );
      const client = createApiClient();

      await client.get("/widgets");

      expect(capturedHeader).toBeNull();
    });

    it("omits X-CSRF-Token on a mutating call when the beecon_csrf cookie is absent (e.g. login, which has no session yet)", async () => {
      let capturedHeader: string | null = "not-yet-captured";
      server.use(
        http.post("/api/v1/widgets", ({ request }) => {
          capturedHeader = request.headers.get("X-CSRF-Token");
          return HttpResponse.json({ id: "widget_1" }, { status: 201 });
        }),
      );
      const client = createApiClient();

      await client.post("/widgets", { name: "Acme" });

      expect(capturedHeader).toBeNull();
    });
  });
});
