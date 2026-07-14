import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { DashboardMetricsSummary } from "@/lib/api-types";

import { DashboardPage } from "./DashboardPage";

function renderDashboard() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <DashboardPage />
    </QueryClientProvider>,
  );
}

function mockDashboardMetrics(summary: DashboardMetricsSummary) {
  server.use(http.get("/api/v1/dashboard/metrics", () => HttpResponse.json(summary)));
}

describe("DashboardPage", () => {
  it("renders the headline metric tiles sourced from GET /dashboard/metrics", async () => {
    mockDashboardMetrics({
      connectionsByStatus: { ACTIVE: 7, INITIATED: 1, EXPIRED: 0, DISCONNECTED: 2 },
      outbox: { pendingDepth: 4, oldestPendingAgeSeconds: 125 },
      deliveryOutcomes: [
        { type: "trigger.event", result: "success", count: 9 },
        { type: "trigger.event", result: "failure", count: 1 },
      ],
    });
    renderDashboard();

    expect(await screen.findByText("7")).toBeInTheDocument(); // Active connections
    expect(screen.getByText("4")).toBeInTheDocument(); // Outbox pending
    expect(screen.getByText("90%")).toBeInTheDocument(); // 9 / (9+1) delivery success rate
    expect(screen.getByText("Active connections")).toBeInTheDocument();
    expect(screen.getByText("Outbox pending")).toBeInTheDocument();
    expect(screen.getByText("Oldest pending event")).toBeInTheDocument();
    expect(screen.getByText("Delivery success rate")).toBeInTheDocument();
  });

  it("shows a sane '—' delivery success rate instead of NaN when no deliveries have ever been attempted", async () => {
    mockDashboardMetrics({
      connectionsByStatus: {},
      outbox: { pendingDepth: 0, oldestPendingAgeSeconds: 0 },
      deliveryOutcomes: [],
    });
    renderDashboard();

    await screen.findByText("Delivery success rate");
    expect(await screen.findByText("—")).toBeInTheDocument();
    expect(screen.queryByText("NaN%")).not.toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/dashboard/metrics", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The dashboard is unavailable." } }, { status: 500 }),
      ),
    );
    renderDashboard();

    expect(await screen.findByText("The dashboard is unavailable.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("labels the connections-by-status chart's bars by status name as text, not color alone", async () => {
    mockDashboardMetrics({
      connectionsByStatus: { ACTIVE: 3, INITIATED: 1, EXPIRED: 0, DISCONNECTED: 2 },
      outbox: { pendingDepth: 0, oldestPendingAgeSeconds: 0 },
      deliveryOutcomes: [],
    });
    renderDashboard();

    for (const label of ["ACTIVE", "INITIATED", "EXPIRED", "DISCONNECTED"]) {
      expect(await screen.findByText(label)).toBeInTheDocument();
    }
  });

  it("labels the delivery-outcomes chart's series with a text legend (Success/Failure), not color alone", async () => {
    mockDashboardMetrics({
      connectionsByStatus: {},
      outbox: { pendingDepth: 0, oldestPendingAgeSeconds: 0 },
      deliveryOutcomes: [
        { type: "trigger.event", result: "success", count: 4 },
        { type: "trigger.event", result: "failure", count: 1 },
      ],
    });
    renderDashboard();

    expect(await screen.findByText("Success")).toBeInTheDocument();
    expect(screen.getByText("Failure")).toBeInTheDocument();
    expect(screen.getByText("trigger.event")).toBeInTheDocument();
  });
});
