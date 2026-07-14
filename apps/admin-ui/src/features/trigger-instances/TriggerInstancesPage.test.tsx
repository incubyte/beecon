import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { TriggerInstance, TriggerInstancesPage as TriggerInstancesPageDTO } from "@/lib/api-types";

import { TriggerInstanceDetailPage } from "./TriggerInstanceDetailPage";
import { TriggerInstancesPage } from "./TriggerInstancesPage";

/** renderTriggerInstancesApp mounts both the list and the full-page detail
 * route in one router tree — TriggerInstancesPage.tsx navigates to
 * "/trigger-instances/$trgId" on row-click (DESIGN.md §0#4: config-heavy
 * surfaces are a full page, not a drawer), so proving that navigation needs
 * the detail route present, not just the list in isolation. */
function renderTriggerInstancesApp(initialPath: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const listRoute = createRoute({ getParentRoute: () => rootRoute, path: "/trigger-instances/", component: TriggerInstancesPage });
  const detailRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/trigger-instances/$trgId",
    component: TriggerInstanceDetailPage,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([listRoute, detailRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return { ...render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>), router };
}

function triggerInstance(overrides: Partial<TriggerInstance>): TriggerInstance {
  return {
    id: "trg_1",
    status: "ACTIVE",
    connectionId: "conn_1",
    triggerSlug: "outlook-message-received",
    config: { folderId: "Inbox" },
    userId: "user_1",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockOrgScopedTriggerInstances(orgId: string, items: TriggerInstance[]) {
  server.use(
    http.get(`/api/v1/organizations/${orgId}/trigger-instances`, () =>
      HttpResponse.json({ items } satisfies TriggerInstancesPageDTO),
    ),
  );
}

describe("TriggerInstancesPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/trigger-instances", () => {
        requested = true;
        return HttpResponse.json({ items: [] } satisfies TriggerInstancesPageDTO);
      }),
    );
    renderTriggerInstancesApp("/trigger-instances/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows the empty state when the selected org has no trigger instances", async () => {
    mockOrgScopedTriggerInstances("org_1", []);
    renderTriggerInstancesApp("/trigger-instances/?org=org_1");

    expect(await screen.findByText(/no trigger instances yet/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/trigger-instances", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Upstream failure." } }, { status: 500 }),
      ),
    );
    renderTriggerInstancesApp("/trigger-instances/?org=org_1");

    expect(await screen.findByText("Upstream failure.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("renders ACTIVE and DISABLED with their paired color, icon, and text label", async () => {
    mockOrgScopedTriggerInstances("org_1", [
      triggerInstance({ id: "trg_active", status: "ACTIVE" }),
      triggerInstance({ id: "trg_disabled", status: "DISABLED" }),
    ]);
    renderTriggerInstancesApp("/trigger-instances/?org=org_1");

    const table = await screen.findByRole("table");
    await within(table).findByText("Active");

    const active = within(table).getByText("Active");
    expect(active.closest("span")).toHaveClass("text-success-text", { exact: false });
    expect(active.closest("span")?.querySelector("svg")).toBeTruthy();

    const disabled = within(table).getByText("Disabled");
    expect(disabled.closest("span")).toHaveClass("text-neutral-text", { exact: false });
    expect(disabled.closest("span")?.querySelector("svg")).toBeTruthy();
  });

  it("opening a row navigates to the full-page detail view", async () => {
    mockOrgScopedTriggerInstances("org_1", [triggerInstance({ id: "trg_open_me", triggerSlug: "outlook-message-received" })]);
    server.use(
      http.get("/api/v1/organizations/org_1/trigger-instances/trg_open_me", () =>
        HttpResponse.json(triggerInstance({ id: "trg_open_me", triggerSlug: "outlook-message-received" })),
      ),
    );
    const { router } = renderTriggerInstancesApp("/trigger-instances/?org=org_1");

    const row = await screen.findByText("trg_open_me", { exact: false });
    row.closest("tr")?.click();

    expect(await screen.findByRole("heading", { name: "outlook-message-received" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/trigger-instances/trg_open_me");
  });

  it("re-scopes to the newly selected org when ?org= changes, never showing the previous org's instances", async () => {
    mockOrgScopedTriggerInstances("org_a", [triggerInstance({ id: "trg_org_a", triggerSlug: "outlook-message-received" })]);
    mockOrgScopedTriggerInstances("org_b", [triggerInstance({ id: "trg_org_b", triggerSlug: "hubspot-contact-created" })]);
    const { router } = renderTriggerInstancesApp("/trigger-instances/?org=org_a");

    await screen.findByText("trg_org_a", { exact: false });
    expect(screen.queryByText("trg_org_b", { exact: false })).not.toBeInTheDocument();

    await router.navigate({ to: "/trigger-instances", search: { org: "org_b" } });

    await screen.findByText("trg_org_b", { exact: false });
    expect(screen.queryByText("trg_org_a", { exact: false })).not.toBeInTheDocument();
  });
});
