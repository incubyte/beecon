import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { LogEntry, LogsPage as LogsPageDTO } from "@/lib/api-types";

import { LogsPage } from "./LogsPage";

/** renderLogsPage mirrors ConnectionsPage.test.tsx's own harness: LogsPage
 * reads the selected org from `?org=` via `useSearch({ from: "__root__" })`,
 * so the harness's root route must carry that same `"__root__"` id. */
function renderLogsPage(initialPath = "/") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: LogsPage });
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

function logEntry(overrides: Partial<LogEntry>): LogEntry {
  return {
    id: "log_1",
    organizationId: "org_1",
    kind: "tool_execution",
    status: 200,
    durationMs: 42,
    requestBody: "{}",
    responseBody: "{}",
    rateLimited: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockOrgScopedLogs(orgId: string, entries: LogEntry[], nextCursor?: string) {
  server.use(
    http.get(`/api/v1/organizations/${orgId}/logs`, () =>
      HttpResponse.json({ entries, ...(nextCursor ? { nextCursor } : {}) } satisfies LogsPageDTO),
    ),
  );
}

describe("LogsPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/logs", () => {
        requested = true;
        return HttpResponse.json({ entries: [] } satisfies LogsPageDTO);
      }),
    );
    renderLogsPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows skeleton loading rows before the first response arrives", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/logs", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ entries: [] } satisfies LogsPageDTO);
      }),
    );
    renderLogsPage("/?org=org_1");

    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("row", { hidden: true }).length).toBeGreaterThan(1);
  });

  it("shows the empty state when the selected org has no log entries", async () => {
    mockOrgScopedLogs("org_1", []);
    renderLogsPage("/?org=org_1");

    expect(await screen.findByText(/no log entries yet/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/logs", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Something went wrong upstream." } }, { status: 500 }),
      ),
    );
    renderLogsPage("/?org=org_1");

    expect(await screen.findByText("Something went wrong upstream.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("sends the connection/user/tool filters as request query params, each as a removable chip", async () => {
    const captured: { params: URLSearchParams | null } = { params: null };
    server.use(
      http.get("/api/v1/organizations/org_1/logs", ({ request }) => {
        captured.params = new URL(request.url).searchParams;
        return HttpResponse.json({ entries: [logEntry({})] } satisfies LogsPageDTO);
      }),
    );
    renderLogsPage("/?org=org_1");
    await screen.findByRole("table");

    fireEvent.change(screen.getByLabelText(/connection id/i), { target: { value: "conn_abc" } });
    await waitFor(() => expect(captured.params?.get("connectionId")).toBe("conn_abc"));

    fireEvent.change(screen.getByLabelText(/user id/i), { target: { value: "user_xyz" } });
    await waitFor(() => expect(captured.params?.get("userId")).toBe("user_xyz"));

    fireEvent.change(screen.getByLabelText(/tool slug/i), { target: { value: "outlook-list-messages" } });
    await waitFor(() => expect(captured.params?.get("toolSlug")).toBe("outlook-list-messages"));

    expect(screen.getByText(/connection: conn_abc/i)).toBeInTheDocument();
    expect(screen.getByText(/user: user_xyz/i)).toBeInTheDocument();
    expect(screen.getByText(/tool: outlook-list-messages/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /remove filter: connection: conn_abc/i }));
    await waitFor(() => expect(captured.params?.has("connectionId")).toBe(false));
    expect(captured.params?.get("userId")).toBe("user_xyz");
  });

  it("sends the from/to date-range filter as request query params", async () => {
    const captured: { params: URLSearchParams | null } = { params: null };
    server.use(
      http.get("/api/v1/organizations/org_1/logs", ({ request }) => {
        captured.params = new URL(request.url).searchParams;
        return HttpResponse.json({ entries: [] } satisfies LogsPageDTO);
      }),
    );
    renderLogsPage("/?org=org_1");
    await screen.findByRole("table");

    fireEvent.change(screen.getByLabelText(/^from$/i), { target: { value: "2026-01-01T00:00" } });
    await waitFor(() => expect(captured.params?.get("from")).toBeTruthy());

    fireEvent.change(screen.getByLabelText(/^to$/i), { target: { value: "2026-01-02T00:00" } });
    await waitFor(() => expect(captured.params?.get("to")).toBeTruthy());
  });

  it("fetches the next page via nextCursor when Load more is clicked, appending rows", async () => {
    let requestedCursor: string | null = null;
    server.use(
      http.get("/api/v1/organizations/org_1/logs", ({ request }) => {
        requestedCursor = new URL(request.url).searchParams.get("cursor");
        if (!requestedCursor) {
          return HttpResponse.json({
            entries: [logEntry({ id: "log_first", connectionId: "conn_first" })],
            nextCursor: "cursor-page-2",
          } satisfies LogsPageDTO);
        }
        return HttpResponse.json({ entries: [logEntry({ id: "log_second", connectionId: "conn_second" })] } satisfies LogsPageDTO);
      }),
    );
    renderLogsPage("/?org=org_1");

    await screen.findByText("conn_first", { exact: false });
    screen.getByRole("button", { name: /load more/i }).click();

    await waitFor(() => expect(requestedCursor).toBe("cursor-page-2"));
    await screen.findByText("conn_second", { exact: false });
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });

  it("opening a row shows the drawer with its redacted request/response bodies via CodeViewer", async () => {
    mockOrgScopedLogs("org_1", [
      logEntry({
        id: "log_click_me",
        connectionId: "conn_click_me",
        requestBody: JSON.stringify({ apiKey: "[REDACTED]" }),
        responseBody: JSON.stringify({ ok: true }),
      }),
    ]);
    renderLogsPage("/?org=org_1");

    const row = await screen.findByText("conn_click_me", { exact: false });
    row.closest("tr")?.click();

    expect(await screen.findByText("Log entry")).toBeInTheDocument();
    expect(screen.getByText("[REDACTED]")).toBeInTheDocument();
    expect(screen.getByText(/"ok": true/)).toBeInTheDocument();
  });
});
