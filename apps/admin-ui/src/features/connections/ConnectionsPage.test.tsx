import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { Connection, ConnectionsPage as ConnectionsPageDTO } from "@/lib/api-types";

import { ConnectionsPage } from "./ConnectionsPage";

/** renderConnectionsPage mounts ConnectionsPage behind a real (minimal)
 * TanStack Router + Query context at the given path — the component reads
 * the selected org from `?org=` via `useSearch({ from: "__root__" })`, so the
 * harness's root route must carry the same `"__root__"` id `createRootRoute`
 * assigns in the real app (routes/__root.tsx). Mirrors
 * OrganizationsPage.test.tsx's own harness shape. */
function renderConnectionsPage(initialPath = "/") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: ConnectionsPage });
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

function connection(overrides: Partial<Connection>): Connection {
  return {
    id: "conn_1",
    status: "ACTIVE",
    providerSlug: "outlook",
    userId: "user_1",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockOrgScopedConnections(orgId: string, items: Connection[], nextCursor?: string) {
  server.use(
    http.get(`/api/v1/organizations/${orgId}/connections`, () =>
      HttpResponse.json({ items, ...(nextCursor ? { nextCursor } : {}) } satisfies ConnectionsPageDTO),
    ),
  );
}

describe("ConnectionsPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/connections", () => {
        requested = true;
        return HttpResponse.json({ items: [] } satisfies ConnectionsPageDTO);
      }),
    );
    renderConnectionsPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows skeleton loading rows before the first response arrives", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/connections", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ items: [] } satisfies ConnectionsPageDTO);
      }),
    );
    renderConnectionsPage("/?org=org_1");

    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("row", { hidden: true }).length).toBeGreaterThan(1);
  });

  it("shows the empty state when the selected org has no connections", async () => {
    mockOrgScopedConnections("org_1", []);
    renderConnectionsPage("/?org=org_1");

    expect(await screen.findByText(/no connections yet/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/connections", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Something went wrong upstream." } }, { status: 500 }),
      ),
    );
    renderConnectionsPage("/?org=org_1");

    expect(await screen.findByText("Something went wrong upstream.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("renders every connection status with its paired color, icon, and text label", async () => {
    mockOrgScopedConnections("org_1", [
      connection({ id: "conn_active", status: "ACTIVE" }),
      connection({ id: "conn_initiated", status: "INITIATED" }),
      connection({ id: "conn_disconnected", status: "DISCONNECTED" }),
      connection({ id: "conn_expired", status: "EXPIRED" }),
    ]);
    renderConnectionsPage("/?org=org_1");

    const table = await screen.findByRole("table");
    await within(table).findByText("Active");
    for (const [label, textClass, bgClass] of [
      ["Active", "text-success-text", "bg-success-bg"],
      ["Initiated", "text-info-text", "bg-info-bg"],
      ["Disconnected", "text-error-text", "bg-error-bg"],
      ["Expired", "text-warning-text", "bg-warning-bg"],
    ] as const) {
      const badge = within(table).getByText(label);
      // Color (background/text token classes) and an icon (an inline svg)
      // are both present alongside the text label — never color alone.
      expect(badge.closest("span")).toHaveClass(textClass, { exact: false });
      expect(badge.closest("span")).toHaveClass(bgClass, { exact: false });
      expect(badge.closest("span")?.querySelector("svg")).toBeTruthy();
    }
  });

  it("fetches the next page via nextCursor when Load more is clicked, appending rows", async () => {
    let requestedCursor: string | null = null;
    server.use(
      http.get("/api/v1/organizations/org_1/connections", ({ request }) => {
        requestedCursor = new URL(request.url).searchParams.get("cursor");
        if (!requestedCursor) {
          return HttpResponse.json({
            items: [connection({ id: "conn_first" })],
            nextCursor: "cursor-page-2",
          } satisfies ConnectionsPageDTO);
        }
        return HttpResponse.json({ items: [connection({ id: "conn_second" })] } satisfies ConnectionsPageDTO);
      }),
    );
    renderConnectionsPage("/?org=org_1");

    await screen.findByText("conn_first", { exact: false });
    screen.getByRole("button", { name: /load more/i }).click();

    await waitFor(() => expect(requestedCursor).toBe("cursor-page-2"));
    await screen.findByText("conn_second", { exact: false });
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });

  it("filters by status with a removable chip, restoring every row when removed", async () => {
    mockOrgScopedConnections("org_1", [
      connection({ id: "conn_active", status: "ACTIVE" }),
      connection({ id: "conn_expired", status: "EXPIRED" }),
    ]);
    renderConnectionsPage("/?org=org_1");
    const table = await screen.findByRole("table");
    await within(table).findByText("Active");
    await within(table).findByText("Expired");

    const statusSelect = screen.getByLabelText(/^status$/i) as HTMLSelectElement;
    fireEvent.change(statusSelect, { target: { value: "ACTIVE" } });

    await waitFor(() => expect(within(table).queryByText("Expired")).not.toBeInTheDocument());
    expect(within(table).getByText("Active")).toBeInTheDocument();
    const chip = screen.getByText(/status: active/i);
    expect(chip).toBeInTheDocument();

    screen.getByRole("button", { name: /remove filter: status: active/i }).click();
    await within(table).findByText("Expired");
    expect(within(table).getByText("Active")).toBeInTheDocument();
  });

  it("filters by integration with a removable chip, restoring every row when removed", async () => {
    mockOrgScopedConnections("org_1", [
      connection({ id: "conn_outlook", providerSlug: "outlook" }),
      connection({ id: "conn_hubspot", providerSlug: "hubspot" }),
    ]);
    renderConnectionsPage("/?org=org_1");
    const table = await screen.findByRole("table");
    await within(table).findByText("outlook");
    await within(table).findByText("hubspot");

    const integrationSelect = screen.getByLabelText(/^integration$/i) as HTMLSelectElement;
    fireEvent.change(integrationSelect, { target: { value: "outlook" } });

    await waitFor(() => expect(within(table).queryByText("hubspot")).not.toBeInTheDocument());
    expect(within(table).getByText("outlook")).toBeInTheDocument();
    expect(screen.getByText(/integration: outlook/i)).toBeInTheDocument();

    screen.getByRole("button", { name: /remove filter: integration: outlook/i }).click();
    await within(table).findByText("hubspot");
  });

  it("opening a row shows the right-side drawer with that connection's id", async () => {
    mockOrgScopedConnections("org_1", [connection({ id: "conn_click_me" })]);
    server.use(
      http.get("/api/v1/organizations/org_1/connections/conn_click_me", () =>
        HttpResponse.json(connection({ id: "conn_click_me" })),
      ),
    );
    renderConnectionsPage("/?org=org_1");

    const row = await screen.findByText("conn_click_me", { exact: false });
    row.closest("tr")?.click();

    expect(await screen.findByText("Connection detail")).toBeInTheDocument();
  });

  it("re-scopes to the newly selected org when ?org= changes, never showing the previous org's connections", async () => {
    mockOrgScopedConnections("org_a", [connection({ id: "conn_org_a", providerSlug: "outlook" })]);
    mockOrgScopedConnections("org_b", [connection({ id: "conn_org_b", providerSlug: "hubspot" })]);

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const rootRoute = createRootRoute({
      validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
    });
    const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: ConnectionsPage });
    const router = createRouter({
      routeTree: rootRoute.addChildren([indexRoute]),
      history: createMemoryHistory({ initialEntries: ["/?org=org_a"] }),
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    await screen.findByText("conn_org_a", { exact: false });
    expect(screen.queryByText("conn_org_b", { exact: false })).not.toBeInTheDocument();

    await router.navigate({ to: "/", search: { org: "org_b" } });

    await screen.findByText("conn_org_b", { exact: false });
    expect(screen.queryByText("conn_org_a", { exact: false })).not.toBeInTheDocument();
  });
});
