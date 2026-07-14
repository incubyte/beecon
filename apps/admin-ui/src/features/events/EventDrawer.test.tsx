import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { DeliveryEvent, LogEntry, LogsPage as LogsPageDTO } from "@/lib/api-types";

import { EventDrawer } from "./EventDrawer";
import { EventsPage } from "./EventsPage";

/** renderDrawer mirrors ConnectionDrawer.test.tsx's own harness: EventDrawer
 * takes orgId/event/onClose as plain props and never reads the router. */
function renderDrawer(event: DeliveryEvent | null, onClose: () => void = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <EventDrawer orgId="org_1" event={event} onClose={onClose} />
    </QueryClientProvider>,
  );
}

function deliveryEvent(overrides: Partial<DeliveryEvent>): DeliveryEvent {
  return {
    id: "evt_1",
    type: "trigger.event",
    createdAt: "2026-01-01T00:00:00.000Z",
    deliveryStatus: "FAILED",
    attempts: 2,
    ...overrides,
  };
}

function logAttempt(overrides: Partial<LogEntry>): LogEntry {
  return {
    id: "log_1",
    organizationId: "org_1",
    kind: "webhook_delivery",
    status: 500,
    durationMs: 12,
    requestBody: "{}",
    responseBody: "{}",
    rateLimited: false,
    eventId: "evt_1",
    attempt: 1,
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockDeliveryAttempts(entries: LogEntry[]) {
  server.use(http.get("/api/v1/organizations/org_1/logs", () => HttpResponse.json({ entries } satisfies LogsPageDTO)));
}

describe("EventDrawer", () => {
  it("is closed when event is null", () => {
    mockDeliveryAttempts([]);
    renderDrawer(null);

    expect(screen.queryByText("Event detail")).not.toBeInTheDocument();
  });

  it("shows id, type, status, attempts, and timestamps for the event", async () => {
    mockDeliveryAttempts([]);
    renderDrawer(deliveryEvent({ deliveryStatus: "FAILED", attempts: 3 }));

    expect(await screen.findByText("Event detail")).toBeInTheDocument();
    expect(screen.getByText("trigger.event")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("sources per-attempt history from the org's logs filtered to this event's own id, sorted by attempt", async () => {
    mockDeliveryAttempts([
      logAttempt({ id: "log_attempt_2", eventId: "evt_1", attempt: 2, status: 500, durationMs: 40 }),
      logAttempt({ id: "log_attempt_1", eventId: "evt_1", attempt: 1, status: 500, durationMs: 20 }),
      logAttempt({ id: "log_other_event", eventId: "evt_other", attempt: 1, status: 200, durationMs: 5 }),
    ]);
    renderDrawer(deliveryEvent({ id: "evt_1" }));

    const table = await screen.findByRole("table");
    const rows = table.querySelectorAll("tbody tr");
    expect(rows.length).toBe(2);
    // Sorted ascending by attempt number: attempt 1 first, attempt 2 second.
    expect(rows[0]).toHaveTextContent("1");
    expect(rows[0]).toHaveTextContent("20 ms");
    expect(rows[1]).toHaveTextContent("2");
    expect(rows[1]).toHaveTextContent("40 ms");
  });

  it("shows 'no delivery attempts recorded yet' when this event has none", async () => {
    mockDeliveryAttempts([]);
    renderDrawer(deliveryEvent({}));

    await screen.findByText("Event detail");
    expect(await screen.findByText(/no delivery attempts recorded yet/i)).toBeInTheDocument();
  });

  it("Redeliver is enabled for a FAILED event and disabled for a DELIVERED one", async () => {
    mockDeliveryAttempts([]);
    renderDrawer(deliveryEvent({ deliveryStatus: "FAILED" }));
    await screen.findByText("Event detail");
    expect(screen.getByRole("button", { name: /^redeliver$/i })).toBeEnabled();

    renderDrawer(deliveryEvent({ deliveryStatus: "DELIVERED" }));
    const buttons = screen.getAllByRole("button", { name: /^redeliver$/i });
    expect(buttons[buttons.length - 1]).toBeDisabled();
  });

  // This one drives the real EventsPage (rather than EventDrawer alone) —
  // useRedeliverEvent's invalidateQueries only auto-refetches queries that
  // are actively observed (TanStack Query's default `refetchType: "active"`
  // on invalidate), so the observable proof that "invalidates the list"
  // actually did something needs a live useEvents() consumer mounted, the
  // same way the drawer only ever appears inside EventsPage in the real app.
  it("Redeliver POSTs to the event's redeliver endpoint and invalidates the list so the row's status updates without a page reload", async () => {
    let redeliverCalled = false;
    mockDeliveryAttempts([]);
    server.use(
      http.post("/api/v1/organizations/org_1/events/evt_1/redeliver", () => {
        redeliverCalled = true;
        return new HttpResponse(null, { status: 202 });
      }),
    );
    // First read reports FAILED; every read after redeliver reports
    // DELIVERED — simulating the background dispatcher having processed the
    // redeliver by the time the invalidated query refetches.
    let listCallCount = 0;
    server.use(
      http.get("/api/v1/organizations/org_1/events", () => {
        listCallCount += 1;
        return HttpResponse.json({
          items: [deliveryEvent({ id: "evt_1", deliveryStatus: listCallCount === 1 ? "FAILED" : "DELIVERED" })],
        });
      }),
    );

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const rootRoute = createRootRoute({
      validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
    });
    const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: EventsPage });
    const router = createRouter({
      routeTree: rootRoute.addChildren([indexRoute]),
      history: createMemoryHistory({ initialEntries: ["/?org=org_1"] }),
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    const row = await screen.findByText("evt_1", { exact: false });
    row.closest("tr")?.click();
    await screen.findByText("Event detail");

    fireEvent.click(screen.getByRole("button", { name: /^redeliver$/i }));

    await waitFor(() => expect(redeliverCalled).toBe(true));
    await waitFor(() => expect(listCallCount).toBeGreaterThan(1));
    await waitFor(() => expect(screen.getAllByText("Delivered").length).toBeGreaterThan(0));
  });

  it("shows an error message when the redeliver request fails", async () => {
    mockDeliveryAttempts([]);
    server.use(
      http.post("/api/v1/organizations/org_1/events/evt_1/redeliver", () =>
        HttpResponse.json({ error: { code: "validation_error", message: "This event cannot be redelivered." } }, { status: 422 }),
      ),
    );
    renderDrawer(deliveryEvent({ id: "evt_1", deliveryStatus: "FAILED" }));
    await screen.findByText("Event detail");

    fireEvent.click(screen.getByRole("button", { name: /^redeliver$/i }));

    expect(await screen.findByText("This event cannot be redelivered.")).toBeInTheDocument();
  });
});
