import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { ApiKeyListing, IssuedApiKey, RotatedApiKey } from "@/lib/api-types";

import { ApiKeysPage } from "./ApiKeysPage";

/** renderApiKeysPage mirrors OrganizationsPage.test.tsx/ConnectionsPage.test.tsx's
 * own harness: ApiKeysPage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderApiKeysPage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: ApiKeysPage });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function key(overrides: Partial<ApiKeyListing> = {}): ApiKeyListing {
  return {
    id: "key_1",
    prefix: "beecon_sk_ab",
    scope: "read-write",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockApiKeysList(items: ApiKeyListing[]) {
  server.use(http.get("/api/v1/organizations/org_1/api-keys", () => HttpResponse.json(items)));
}

describe("ApiKeysPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/api-keys", () => {
        requested = true;
        return HttpResponse.json([]);
      }),
    );
    renderApiKeysPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows the empty state when the org has no keys", async () => {
    mockApiKeysList([]);
    renderApiKeysPage();

    expect(await screen.findByText(/no api keys yet/i)).toBeInTheDocument();
  });

  it("renders prefix, scope badge, and status badge per row — never a secret anywhere in the table", async () => {
    mockApiKeysList([key({ id: "key_ro", prefix: "beecon_sk_ro", scope: "read-only" })]);
    renderApiKeysPage();

    // DataTable always renders a <table> element, even mid-loading (its
    // skeleton rows live inside one) — waiting on the row's own prefix text
    // is what actually confirms the real data landed, not just that some
    // table exists.
    await screen.findByText("beecon_sk_ro");
    const table = screen.getByRole("table");
    expect(within(table).getByText(/read-only/i)).toBeInTheDocument();
    expect(within(table).getByText(/active/i)).toBeInTheDocument();
    // Adversarial: the table must never render anything that looks like the
    // full secret value (it isn't in ApiKeyListing at all, but this guards
    // against a future regression that accidentally serializes one in).
    expect(within(table).queryByText(/beecon_sk_the-full-secret/i)).not.toBeInTheDocument();
  });

  it("shows a revoked key's status as Revoked", async () => {
    mockApiKeysList([key({ id: "key_1", revokedAt: "2026-02-01T00:00:00.000Z" })]);
    renderApiKeysPage();

    expect(await screen.findByText("Revoked")).toBeInTheDocument();
  });

  it("shows a just-rotated key (inside its overlap window) as Rotating", async () => {
    const future = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    mockApiKeysList([key({ id: "key_1", rotatedAt: "2026-01-01T00:00:00.000Z", overlapExpiresAt: future })]);
    renderApiKeysPage();

    expect(await screen.findByText("Rotating")).toBeInTheDocument();
  });

  it("creating a key opens SecretOnceModal exactly once with the returned secret", async () => {
    mockApiKeysList([]);
    server.use(
      http.post("/api/v1/organizations/org_1/api-keys", () =>
        HttpResponse.json(
          { id: "key_new", key: "beecon_sk_freshly-issued", prefix: "beecon_sk_fr", scope: "read-write", createdAt: "2026-01-01T00:00:00.000Z" } satisfies IssuedApiKey,
          { status: 201 },
        ),
      ),
    );
    renderApiKeysPage();
    await screen.findByText(/no api keys yet/i);

    fireEvent.click(screen.getByRole("button", { name: /create api key/i }));
    fireEvent.click(await screen.findByRole("button", { name: /create key/i }));

    expect(await screen.findByText("beecon_sk_freshly-issued")).toBeInTheDocument();
    // Exactly one SecretOnceModal instance is showing the secret — a second,
    // stray render would duplicate the text node.
    expect(screen.getAllByText("beecon_sk_freshly-issued")).toHaveLength(1);
  });

  it("rotating a key opens SecretOnceModal with the newly rotated secret", async () => {
    mockApiKeysList([key({ id: "key_1" })]);
    server.use(
      http.post("/api/v1/organizations/org_1/api-keys/key_1/rotate", () =>
        HttpResponse.json(
          { id: "key_1", key: "beecon_sk_rotated-secret", prefix: "beecon_sk_ro", overlapExpiresAt: "2026-01-02T00:00:00.000Z" } satisfies RotatedApiKey,
          { status: 201 },
        ),
      ),
    );
    renderApiKeysPage();

    fireEvent.click(await screen.findByRole("button", { name: /^rotate$/i }));

    expect(await screen.findByText("beecon_sk_rotated-secret")).toBeInTheDocument();
  });

  it("revoking a key is gated by TypeToConfirm on the key's own id", async () => {
    mockApiKeysList([key({ id: "key_to_revoke" })]);
    let revokeRequested = false;
    server.use(
      http.delete("/api/v1/organizations/org_1/api-keys/key_to_revoke", () => {
        revokeRequested = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApiKeysPage();

    fireEvent.click(await screen.findByRole("button", { name: /^revoke$/i }));
    const confirmButton = await screen.findByRole("button", { name: /revoke key/i });
    // The confirm button stays disabled until the operator types the exact
    // key id — clicking it before typing must not revoke anything.
    fireEvent.click(confirmButton);
    expect(revokeRequested).toBe(false);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "key_to_revoke" } });
    fireEvent.click(screen.getByRole("button", { name: /revoke key/i }));

    await waitFor(() => expect(revokeRequested).toBe(true));
  });

  it("does not revoke when a wrong value is typed into the confirm field", async () => {
    mockApiKeysList([key({ id: "key_to_revoke" })]);
    let revokeRequested = false;
    server.use(
      http.delete("/api/v1/organizations/org_1/api-keys/key_to_revoke", () => {
        revokeRequested = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderApiKeysPage();

    fireEvent.click(await screen.findByRole("button", { name: /^revoke$/i }));
    await screen.findByRole("button", { name: /revoke key/i });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "wrong-id" } });

    expect(screen.getByRole("button", { name: /revoke key/i })).toBeDisabled();
    expect(revokeRequested).toBe(false);
  });
});
