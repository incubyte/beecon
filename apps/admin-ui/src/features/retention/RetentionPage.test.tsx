import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { Retention } from "@/lib/api-types";

import { RetentionPage } from "./RetentionPage";

/** renderRetentionPage mirrors GovernancePage.test.tsx's own harness:
 * RetentionPage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderRetentionPage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: RetentionPage });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return {
    router,
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  };
}

function retention(overrides: Partial<Retention> = {}): Retention {
  return {
    logDays: null,
    eventDays: null,
    installationDefaultDays: 30,
    ...overrides,
  };
}

function mockRetention(orgId: string, ret: Retention) {
  server.use(http.get(`/api/v1/organizations/${orgId}/retention`, () => HttpResponse.json(ret)));
}

/** waitForLoaded resolves once the Logs section's legend is on screen —
 * the retention form seeds asynchronously from the fetched data (Slice 7
 * mirrors GovernancePage's own seed-on-data-arrival effect). */
async function waitForLoaded() {
  return screen.findByText("Logs");
}

describe("RetentionPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/retention", () => {
        requested = true;
        return HttpResponse.json(retention());
      }),
    );
    renderRetentionPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows a loading state before the retention windows arrive", async () => {
    server.use(
      http.get(
        "/api/v1/organizations/org_1/retention",
        () => new Promise(() => {}), // never resolves within this test
      ),
    );
    renderRetentionPage();

    expect(await screen.findByText(/loading retention settings/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the retention request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/retention", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Retention settings could not be loaded." } }, { status: 500 }),
      ),
    );
    renderRetentionPage();

    expect(await screen.findByText("Retention settings could not be loaded.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("renders both null windows as 'inherit installation default (N)' clearly naming the current default", async () => {
    mockRetention("org_1", retention({ logDays: null, eventDays: null, installationDefaultDays: 30 }));
    renderRetentionPage();
    await waitForLoaded();

    const inheritLabels = screen.getAllByText(/inherit installation default/i);
    expect(inheritLabels.length).toBeGreaterThanOrEqual(2); // one per field (logs, events)
    expect(screen.getAllByText("30").length).toBeGreaterThanOrEqual(2);

    const inheritRadios = screen.getAllByRole("radio", { checked: true });
    expect(inheritRadios).toHaveLength(2); // both fields default to "inherit"
  });

  it("renders a 0 window as the selected 'unlimited' option, distinct from inherit", async () => {
    mockRetention("org_1", retention({ logDays: 0, eventDays: 14, installationDefaultDays: 30 }));
    renderRetentionPage();
    await waitForLoaded();

    const unlimitedRadios = screen.getAllByRole("radio", { name: /unlimited/i });
    expect(unlimitedRadios[0]).toBeChecked();
    expect(screen.getAllByText(/never purge, disables purging for this organization/i).length).toBeGreaterThan(0);
  });

  it("renders a positive window as the selected 'custom' option showing its day count", async () => {
    mockRetention("org_1", retention({ logDays: 14, eventDays: null, installationDefaultDays: 30 }));
    renderRetentionPage();
    await waitForLoaded();

    const customRadios = screen.getAllByRole("radio", { name: /custom window/i });
    expect(customRadios[0]).toBeChecked();
    expect(screen.getByRole("spinbutton", { name: /days to keep/i })).toHaveValue(14);
  });

  it("PUTs logDays: null and eventDays: 0 when the operator picks inherit for logs and unlimited for events", async () => {
    mockRetention("org_1", retention({ logDays: 14, eventDays: 14, installationDefaultDays: 30 }));
    let putBody: unknown;
    server.use(
      http.put("/api/v1/organizations/org_1/retention", async ({ request }) => {
        putBody = await request.json();
        return HttpResponse.json(retention({ logDays: null, eventDays: 0 }));
      }),
    );
    renderRetentionPage();
    await waitForLoaded();

    const inheritRadios = screen.getAllByRole("radio", { name: /inherit installation default/i });
    fireEvent.click(inheritRadios[0]); // Logs -> inherit
    const unlimitedRadios = screen.getAllByRole("radio", { name: /unlimited/i });
    fireEvent.click(unlimitedRadios[1]); // Events -> unlimited
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(putBody).toEqual({ logDays: null, eventDays: 0 }));
  });

  it("PUTs the custom day count when the operator selects Custom and edits the day input", async () => {
    mockRetention("org_1", retention({ logDays: null, eventDays: null, installationDefaultDays: 30 }));
    let putBody: unknown;
    server.use(
      http.put("/api/v1/organizations/org_1/retention", async ({ request }) => {
        putBody = await request.json();
        return HttpResponse.json(retention({ logDays: 21 }));
      }),
    );
    renderRetentionPage();
    await waitForLoaded();

    const customRadios = screen.getAllByRole("radio", { name: /custom window/i });
    fireEvent.click(customRadios[0]); // Logs -> custom
    const dayInput = await screen.findByRole("spinbutton", { name: /days to keep/i });
    fireEvent.change(dayInput, { target: { value: "21" } });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(putBody).toMatchObject({ logDays: 21 }));
  });

  // TestUpdateRetention_Returns422ForAWindowBelowTheMinimum's frontend
  // mirror (server/internal/organizations/driving/httpapi/
  // retention_handler_test.go): a server-side validation rejection surfaces
  // inline in the save bar rather than silently failing or navigating away.
  it("surfaces a sub-minimum validation error from the server inline in the save bar", async () => {
    mockRetention("org_1", retention({ logDays: null, eventDays: null, installationDefaultDays: 30 }));
    server.use(
      http.put("/api/v1/organizations/org_1/retention", () =>
        HttpResponse.json(
          { error: { code: "validation_failed", message: "logRetentionDays must be 0 (unlimited) or at least 1 day(s)" } },
          { status: 422 },
        ),
      ),
    );
    renderRetentionPage();
    await waitForLoaded();

    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    expect(await screen.findByText(/must be 0 \(unlimited\) or at least 1 day\(s\)/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save changes/i })).toBeInTheDocument();
  });

  it("re-scopes to the newly selected org when ?org= changes, never mixing the previous org's retention windows", async () => {
    mockRetention("org_a", retention({ logDays: 7, eventDays: null, installationDefaultDays: 30 }));
    mockRetention("org_b", retention({ logDays: null, eventDays: 0, installationDefaultDays: 30 }));

    const { router } = renderRetentionPage("/?org=org_a");
    await waitForLoaded();
    expect(screen.getAllByRole("radio", { name: /custom window/i })[0]).toBeChecked();

    await router.navigate({ to: "/", search: { org: "org_b" } });
    await waitForLoaded();

    expect(screen.getAllByRole("radio", { name: /inherit installation default/i })[0]).toBeChecked();
    expect(screen.getAllByRole("radio", { name: /unlimited/i })[1]).toBeChecked();
  });

  it("exposes each retention field as an accessible radiogroup-style fieldset with a named legend", async () => {
    mockRetention("org_1", retention());
    renderRetentionPage();
    await waitForLoaded();

    // Every radio option has an accessible name derived from its own label
    // text (not color- or position-only), and both fields (Logs, Events)
    // are present as distinct groups.
    expect(screen.getByText("Logs")).toBeInTheDocument();
    expect(screen.getByText("Events")).toBeInTheDocument();
    expect(screen.getAllByRole("radio", { name: /inherit installation default/i })).toHaveLength(2);
    expect(screen.getAllByRole("radio", { name: /custom window/i })).toHaveLength(2);
    expect(screen.getAllByRole("radio", { name: /unlimited/i })).toHaveLength(2);
  });
});
