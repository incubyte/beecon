import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { TriggerInstance, TriggerInstanceStatusResult } from "@/lib/api-types";

import { TriggerInstanceDetailPage } from "./TriggerInstanceDetailPage";
import { TriggerInstancesPage } from "./TriggerInstancesPage";

function renderDetailApp(initialPath: string) {
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
    id: "trg_detail_1",
    status: "ACTIVE",
    connectionId: "conn_1",
    triggerSlug: "outlook-message-received",
    config: { folderId: "Inbox" },
    userId: "user_1",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockInstanceDetail(instance: TriggerInstance) {
  server.use(http.get(`/api/v1/organizations/org_1/trigger-instances/${instance.id}`, () => HttpResponse.json(instance)));
}

describe("TriggerInstanceDetailPage", () => {
  it("shows status, trigger slug, bound connection, and config as JSON", async () => {
    mockInstanceDetail(triggerInstance({ config: { folderId: "Inbox", pageSize: 25 } }));
    renderDetailApp("/trigger-instances/trg_detail_1?org=org_1");

    expect(await screen.findByRole("heading", { name: "outlook-message-received" })).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByTitle("conn_1")).toBeInTheDocument();
    expect(screen.getByText(/"folderId": "Inbox"/)).toBeInTheDocument();
    expect(screen.getByText(/"pageSize": 25/)).toBeInTheDocument();
  });

  it("toggling disable optimistically flips the badge before the server responds", async () => {
    mockInstanceDetail(triggerInstance({ status: "ACTIVE" }));
    let resolveDisable!: () => void;
    server.use(
      http.post("/api/v1/organizations/org_1/trigger-instances/trg_detail_1/disable", async () => {
        await new Promise<void>((resolve) => {
          resolveDisable = resolve;
        });
        return HttpResponse.json({ id: "trg_detail_1", status: "DISABLED" } satisfies TriggerInstanceStatusResult);
      }),
    );
    renderDetailApp("/trigger-instances/trg_detail_1?org=org_1");

    await screen.findByRole("button", { name: /^disable$/i });
    fireEvent.click(screen.getByRole("button", { name: /^disable$/i }));

    // The badge flips to Disabled immediately (optimistic), before the
    // in-flight POST above has been allowed to resolve.
    await waitFor(() => expect(screen.getByText("Disabled")).toBeInTheDocument());
    expect(screen.queryByText("Active")).not.toBeInTheDocument();

    resolveDisable();
    await waitFor(() => expect(screen.getByRole("button", { name: /^enable$/i })).toBeInTheDocument());
  });

  it("a failed toggle rolls back the optimistic status", async () => {
    mockInstanceDetail(triggerInstance({ status: "ACTIVE" }));
    server.use(
      http.post("/api/v1/organizations/org_1/trigger-instances/trg_detail_1/disable", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "failed" } }, { status: 500 }),
      ),
    );
    renderDetailApp("/trigger-instances/trg_detail_1?org=org_1");

    await screen.findByRole("button", { name: /^disable$/i });
    fireEvent.click(screen.getByRole("button", { name: /^disable$/i }));

    await waitFor(() => expect(screen.getByText("Active")).toBeInTheDocument());
  });

  it("delete requires a ConfirmDialog and navigates back to the list on success", async () => {
    mockInstanceDetail(triggerInstance({}));
    let deleteCalled = false;
    server.use(
      http.delete("/api/v1/organizations/org_1/trigger-instances/trg_detail_1", () => {
        deleteCalled = true;
        return new HttpResponse(null, { status: 204 });
      }),
      http.get("/api/v1/organizations/org_1/trigger-instances", () => HttpResponse.json({ items: [] })),
    );
    const { router } = renderDetailApp("/trigger-instances/trg_detail_1?org=org_1");

    await screen.findByRole("button", { name: /delete trigger instance/i });
    // Clicking the page's own delete button must not delete anything by
    // itself — a confirm dialog gates the action.
    fireEvent.click(screen.getByRole("button", { name: /delete trigger instance/i }));
    expect(deleteCalled).toBe(false);

    await screen.findByText("Delete this trigger instance?");
    const confirmButtons = screen.getAllByRole("button", { name: /delete trigger instance/i });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(deleteCalled).toBe(true));
    await waitFor(() => expect(router.state.location.pathname).toBe("/trigger-instances"));
  });
});
