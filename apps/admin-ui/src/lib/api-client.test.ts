import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";

import { ApiError, createApiClient, verifyAdminKey } from "./api-client";
import { clearAdminKey, getAdminKey, setAdminKey } from "./auth";

afterEach(() => {
  clearAdminKey();
});

describe("createApiClient", () => {
  it("injects Authorization: Bearer <key> from the supplied key store on every request", async () => {
    let capturedAuthHeader: string | null = null;
    server.use(
      http.get("/api/v1/widgets", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return HttpResponse.json({ items: [] });
      }),
    );
    const client = createApiClient({ getKey: () => "beecon_admin_the-key" });

    await client.get("/widgets");

    expect(capturedAuthHeader).toBe("Bearer beecon_admin_the-key");
  });

  it("sends no Authorization header when there is no key", async () => {
    let capturedAuthHeader: string | null = "not-yet-captured";
    server.use(
      http.get("/api/v1/widgets", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return HttpResponse.json({ items: [] });
      }),
    );
    const client = createApiClient({ getKey: () => null });

    await client.get("/widgets");

    expect(capturedAuthHeader).toBeNull();
  });

  it("resolves GET with the parsed JSON body", async () => {
    server.use(http.get("/api/v1/widgets", () => HttpResponse.json({ items: ["a", "b"] })));
    const client = createApiClient({ getKey: () => "k" });

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
    const client = createApiClient({ getKey: () => "k" });

    await client.post("/widgets", { name: "Acme" });

    expect(capturedBody).toEqual({ name: "Acme" });
  });

  // --- 401 handling (Slice 1, AC7: an API call that returns 401 sends the
  // operator back to the gate). ---

  it("calls onUnauthorized when a request returns 401", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 401 })));
    let unauthorizedCalls = 0;
    const client = createApiClient({ getKey: () => "k", onUnauthorized: () => unauthorizedCalls++ });

    await client.get("/widgets").catch(() => {});

    expect(unauthorizedCalls).toBe(1);
  });

  it("does not call onUnauthorized for a non-401 error", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 500 })));
    let unauthorizedCalls = 0;
    const client = createApiClient({ getKey: () => "k", onUnauthorized: () => unauthorizedCalls++ });

    await client.get("/widgets").catch(() => {});

    expect(unauthorizedCalls).toBe(0);
  });

  // A client built with no overrides wires the real in-memory auth store
  // (createApiClient's own defaults: getKey ?? getAdminKey, onUnauthorized ??
  // clearAdminKey) — this pins the real end-to-end wiring Slice 1 depends
  // on: a 401 through the real store actually clears the key the gate
  // reads, not just a test double standing in for it. (Built inside the
  // test body, not imported as the module-level `apiClient` singleton: the
  // singleton is constructed at module-import time, before MSW's
  // server.listen() has patched the global fetch, and so permanently binds
  // the pre-interception fetch — an MSW/module-timing artifact of static
  // imports, not something a real page load ever hits.)
  it("a client wired with the real auth store clears the in-memory admin key on a 401", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 401 })));
    setAdminKey("beecon_admin_will-be-cleared");
    const realClient = createApiClient();

    await realClient.get("/widgets").catch(() => {});

    expect(getAdminKey()).toBeNull();
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
    const client = createApiClient({ getKey: () => "k" });

    await expect(client.get("/widgets")).rejects.toMatchObject({
      status: 422,
      code: "validation_failed",
      message: "validation failed",
      details: { field: "name" },
    });
  });

  it("throws an ApiError instance specifically, not a plain object", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 500 })));
    const client = createApiClient({ getKey: () => "k" });

    await expect(client.get("/widgets")).rejects.toBeInstanceOf(ApiError);
  });

  it("falls back to a generic error shape when the error body isn't valid JSON", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse("not json", { status: 500 })));
    const client = createApiClient({ getKey: () => "k" });

    await expect(client.get("/widgets")).rejects.toMatchObject({ status: 500, code: "unknown_error" });
  });

  it("returns undefined for a 204 No Content response", async () => {
    server.use(http.get("/api/v1/widgets", () => new HttpResponse(null, { status: 204 })));
    const client = createApiClient({ getKey: () => "k" });

    const result = await client.get("/widgets");

    expect(result).toBeUndefined();
  });

  // BUG FOUND DURING SLICE 3 TESTING (not fixed here — reported to the
  // developer/slice-coder): request() only special-cases HTTP 204 before
  // calling response.json(); a 202 Accepted with an empty body — exactly
  // what the real backend returns for POST .../events/{evtId}/redeliver
  // (delivery.Handler.Redeliver, confirmed 202 with a 0-byte body by the Go
  // integration test's own request log) — makes response.json() throw
  // "Unexpected end of JSON input" on the empty string, so
  // useRedeliverEvent's mutation (features/events/api.ts) always reports
  // isError even when the server accepted the redeliver. This test encodes
  // the INTENDED behavior (resolve, don't throw, for any empty-bodied 2xx —
  // not just 204) and is expected to fail until request() is fixed to treat
  // "no body to parse" the same way for every empty-bodied success status,
  // not only 204.
  it("BUG: resolves (does not throw) for a 202 Accepted response with an empty body", async () => {
    server.use(http.post("/api/v1/widgets", () => new HttpResponse(null, { status: 202 })));
    const client = createApiClient({ getKey: () => "k" });

    await expect(client.post("/widgets")).resolves.toBeUndefined();
  });
});

describe("verifyAdminKey", () => {
  it("resolves true for a 204 response", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));

    await expect(verifyAdminKey("any-key")).resolves.toBe(true);
  });

  it("resolves false for a 401 response", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 401 })));

    await expect(verifyAdminKey("any-key")).resolves.toBe(false);
  });

  it("resolves false (never throws) when the network request itself fails", async () => {
    server.use(
      http.get("/admin/verify", () => {
        throw new Error("network down");
      }),
    );

    await expect(verifyAdminKey("any-key")).resolves.toBe(false);
  });

  it("sends the candidate key as the Bearer token, not whatever is in the in-memory store", async () => {
    setAdminKey("beecon_admin_currently-stored");
    let capturedAuthHeader: string | null = null;
    server.use(
      http.get("/admin/verify", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await verifyAdminKey("beecon_admin_candidate");

    expect(capturedAuthHeader).toBe("Bearer beecon_admin_candidate");
  });
});
