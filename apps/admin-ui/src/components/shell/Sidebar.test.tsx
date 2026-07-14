import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Sidebar } from "./Sidebar";

/** renderSidebarAt mounts Sidebar under a minimal router carrying the
 * OPERATE routes (Connections/Trigger Instances) plus the shared `?org=`
 * search param — proving the sidebar's own links preserve the selected org
 * (architecture doc §2.4: the org lives in the URL, every org-scoped view
 * reads it, so a nav link that dropped it would silently de-scope the next
 * page). */
function renderSidebarAt(initialPath: string) {
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
    component: Sidebar,
  });
  const connectionsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/connections", component: () => null });
  const triggerInstancesRoute = createRoute({ getParentRoute: () => rootRoute, path: "/trigger-instances", component: () => null });
  const dashboardRoute = createRoute({ getParentRoute: () => rootRoute, path: "/dashboard", component: () => null });
  const logsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/logs", component: () => null });
  const eventsRoute = createRoute({ getParentRoute: () => rootRoute, path: "/events", component: () => null });
  const usersRoute = createRoute({ getParentRoute: () => rootRoute, path: "/users", component: () => null });
  const apiKeysRoute = createRoute({ getParentRoute: () => rootRoute, path: "/api-keys", component: () => null });
  const governanceRoute = createRoute({ getParentRoute: () => rootRoute, path: "/governance", component: () => null });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      connectionsRoute,
      triggerInstancesRoute,
      dashboardRoute,
      logsRoute,
      eventsRoute,
      usersRoute,
      apiKeysRoute,
      governanceRoute,
    ]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return render(<RouterProvider router={router} />);
}

describe("Sidebar", () => {
  it("Connections and Trigger Instances links preserve the selected org from the current URL", async () => {
    renderSidebarAt("/connections?org=org_42");

    expect(await screen.findByRole("link", { name: /connections/i })).toHaveAttribute("href", "/connections?org=org_42");
    expect(screen.getByRole("link", { name: /trigger instances/i })).toHaveAttribute("href", "/trigger-instances?org=org_42");
  });

  it("links carry no org param when none is selected", async () => {
    renderSidebarAt("/connections");

    expect(await screen.findByRole("link", { name: /connections/i })).toHaveAttribute("href", "/connections");
  });

  it("every other CATALOG/ADMINISTER/GOVERN area not yet wired renders as a disabled, non-link item", async () => {
    renderSidebarAt("/connections?org=org_42");

    // Dashboard/Logs/Events & Delivery are wired as of Slice 3 — Metrics
    // (still unwired) is this test's stand-in for "not yet built".
    const metrics = await screen.findByText("Metrics");
    expect(metrics.closest('[aria-disabled="true"]')).not.toBeNull();
    expect(screen.queryByRole("link", { name: /metrics/i })).not.toBeInTheDocument();
  });

  // Slice 3: the OBSERVE group's three new links.
  it("Logs and Events & Delivery links preserve the selected org from the current URL, like Connections", async () => {
    renderSidebarAt("/connections?org=org_42");

    expect(await screen.findByRole("link", { name: /^logs$/i })).toHaveAttribute("href", "/logs?org=org_42");
    expect(screen.getByRole("link", { name: /events & delivery/i })).toHaveAttribute("href", "/events?org=org_42");
  });

  it("the Dashboard link points at /dashboard — the installation-wide surface every other OBSERVE/OPERATE link sits above", async () => {
    renderSidebarAt("/connections?org=org_42");

    const dashboardLink = await screen.findByRole("link", { name: /^dashboard$/i });
    expect(dashboardLink.getAttribute("href")).toMatch(/^\/dashboard/);
  });

  // Slice 4: the ADMINISTER group's two new links (end-users and
  // scope-restricted API keys) — the same org-preserving precedent every
  // other org-scoped nav link above already pins.
  it("Users and API Keys links preserve the selected org from the current URL, like Connections", async () => {
    renderSidebarAt("/connections?org=org_42");

    expect(await screen.findByRole("link", { name: /^users$/i })).toHaveAttribute("href", "/users?org=org_42");
    expect(screen.getByRole("link", { name: /api keys/i })).toHaveAttribute("href", "/api-keys?org=org_42");
  });

  it("Users and API Keys links carry no org param when none is selected", async () => {
    renderSidebarAt("/connections");

    expect(await screen.findByRole("link", { name: /^users$/i })).toHaveAttribute("href", "/users");
    expect(screen.getByRole("link", { name: /api keys/i })).toHaveAttribute("href", "/api-keys");
  });

  // Slice 5: the GOVERN group's Governance link (the core-risk seam's
  // editor) — the same org-preserving precedent every other org-scoped nav
  // link above already pins, since governance is itself org-scoped.
  it("the Governance link preserves the selected org from the current URL, like every other org-scoped link", async () => {
    renderSidebarAt("/connections?org=org_42");

    expect(await screen.findByRole("link", { name: /^governance$/i })).toHaveAttribute("href", "/governance?org=org_42");
  });

  it("the Governance link carries no org param when none is selected", async () => {
    renderSidebarAt("/connections");

    expect(await screen.findByRole("link", { name: /^governance$/i })).toHaveAttribute("href", "/governance");
  });
});
