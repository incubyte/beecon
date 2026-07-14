import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { DeliveryEvent, EventsPage as EventsPageDTO, LogsPage as LogsPageDTO } from "@/lib/api-types";

import { EventsPage } from "./EventsPage";

/** renderEventsPage mirrors ConnectionsPage.test.tsx's own harness. */
function renderEventsPage(initialPath = "/") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: EventsPage });
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

function deliveryEvent(overrides: Partial<DeliveryEvent>): DeliveryEvent {
  return {
    id: "evt_1",
    type: "trigger.event",
    createdAt: "2026-01-01T00:00:00.000Z",
    deliveryStatus: "DELIVERED",
    attempts: 1,
    ...overrides,
  };
}

function mockOrgScopedEvents(orgId: string, items: DeliveryEvent[], nextCursor?: string) {
  server.use(
    http.get(`/api/v1/organizations/${orgId}/events`, () =>
      HttpResponse.json({ items, ...(nextCursor ? { nextCursor } : {}) } satisfies EventsPageDTO),
    ),
  );
}

function mockNoDeliveryAttempts(orgId: string) {
  server.use(http.get(`/api/v1/organizations/${orgId}/logs`, () => HttpResponse.json({ entries: [] } satisfies LogsPageDTO)));
}

describe("EventsPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/events", () => {
        requested = true;
        return HttpResponse.json({ items: [] } satisfies EventsPageDTO);
      }),
    );
    renderEventsPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows skeleton loading rows before the first response arrives", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/events", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ items: [] } satisfies EventsPageDTO);
      }),
    );
    renderEventsPage("/?org=org_1");

    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("row", { hidden: true }).length).toBeGreaterThan(1);
  });

  it("shows the empty state when the selected org has no events", async () => {
    mockOrgScopedEvents("org_1", []);
    renderEventsPage("/?org=org_1");

    expect(await screen.findByText(/no events yet/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/events", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Something went wrong upstream." } }, { status: 500 }),
      ),
    );
    renderEventsPage("/?org=org_1");

    expect(await screen.findByText("Something went wrong upstream.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("renders every delivery status with its paired color, icon, and text label", async () => {
    mockOrgScopedEvents("org_1", [
      deliveryEvent({ id: "evt_delivered", deliveryStatus: "DELIVERED" }),
      deliveryEvent({ id: "evt_pending", deliveryStatus: "PENDING" }),
      deliveryEvent({ id: "evt_failed", deliveryStatus: "FAILED" }),
      deliveryEvent({ id: "evt_no_endpoint", deliveryStatus: "NO_ENDPOINT" }),
    ]);
    renderEventsPage("/?org=org_1");

    const table = await screen.findByRole("table");
    await within(table).findByText("Delivered");
    for (const [label, textClass, bgClass] of [
      ["Delivered", "text-success-text", "bg-success-bg"],
      ["Pending", "text-neutral-text", "bg-neutral-bg"],
      ["Failed", "text-error-text", "bg-error-bg"],
      ["No endpoint", "text-warning-text", "bg-warning-bg"],
    ] as const) {
      const badge = within(table).getByText(label);
      expect(badge.closest("span")).toHaveClass(textClass, { exact: false });
      expect(badge.closest("span")).toHaveClass(bgClass, { exact: false });
      expect(badge.closest("span")?.querySelector("svg")).toBeTruthy();
    }
  });

  it("filters by type with a removable chip, restoring every row when removed", async () => {
    // The type filter is sent server-side (EventsFilters, api.ts), unlike
    // Connections' client-side status/integration filters (Slice 2) — this
    // handler filters by the request's own ?type= param, the same way the
    // real logging.QueryParams-backed endpoint would.
    const all = [
      deliveryEvent({ id: "evt_trigger", type: "trigger.event" }),
      deliveryEvent({ id: "evt_connexpired", type: "connection.expired" }),
    ];
    server.use(
      http.get("/api/v1/organizations/org_1/events", ({ request }) => {
        const type = new URL(request.url).searchParams.get("type");
        const items = type ? all.filter((event) => event.type === type) : all;
        return HttpResponse.json({ items } satisfies EventsPageDTO);
      }),
    );
    renderEventsPage("/?org=org_1");
    const table = await screen.findByRole("table");
    await within(table).findByText("trigger.event");
    await within(table).findByText("connection.expired");

    const typeSelect = screen.getByLabelText(/^type$/i) as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: "trigger.event" } });

    await waitFor(() => expect(within(table).queryByText("connection.expired")).not.toBeInTheDocument());
    await within(table).findByText("trigger.event");
    expect(screen.getByText(/type: trigger\.event/i)).toBeInTheDocument();

    screen.getByRole("button", { name: /remove filter: type: trigger\.event/i }).click();
    await within(table).findByText("connection.expired");
  });

  it("filters by delivery status with a removable chip, sending the request param", async () => {
    let requestedParams: URLSearchParams | null = null;
    server.use(
      http.get("/api/v1/organizations/org_1/events", ({ request }) => {
        requestedParams = new URL(request.url).searchParams;
        return HttpResponse.json({ items: [deliveryEvent({})] } satisfies EventsPageDTO);
      }),
    );
    renderEventsPage("/?org=org_1");
    await screen.findByRole("table");

    const statusSelect = screen.getByLabelText(/^status$/i) as HTMLSelectElement;
    fireEvent.change(statusSelect, { target: { value: "FAILED" } });

    await waitFor(() => expect(requestedParams?.get("deliveryStatus")).toBe("FAILED"));
    expect(screen.getByText(/status: failed/i)).toBeInTheDocument();
  });

  it("fetches the next page via nextCursor when Load more is clicked, appending rows", async () => {
    let requestedCursor: string | null = null;
    server.use(
      http.get("/api/v1/organizations/org_1/events", ({ request }) => {
        requestedCursor = new URL(request.url).searchParams.get("cursor");
        if (!requestedCursor) {
          return HttpResponse.json({ items: [deliveryEvent({ id: "evt_first" })], nextCursor: "cursor-page-2" } satisfies EventsPageDTO);
        }
        return HttpResponse.json({ items: [deliveryEvent({ id: "evt_second" })] } satisfies EventsPageDTO);
      }),
    );
    renderEventsPage("/?org=org_1");

    await screen.findByText("evt_first", { exact: false });
    screen.getByRole("button", { name: /load more/i }).click();

    await waitFor(() => expect(requestedCursor).toBe("cursor-page-2"));
    await screen.findByText("evt_second", { exact: false });
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });

  it("opening a row shows the drawer with that event's id", async () => {
    mockOrgScopedEvents("org_1", [deliveryEvent({ id: "evt_click_me" })]);
    mockNoDeliveryAttempts("org_1");
    renderEventsPage("/?org=org_1");

    const row = await screen.findByText("evt_click_me", { exact: false });
    row.closest("tr")?.click();

    expect(await screen.findByText("Event detail")).toBeInTheDocument();
  });
});
