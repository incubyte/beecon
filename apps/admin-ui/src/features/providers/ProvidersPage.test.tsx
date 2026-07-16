import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { ProviderDefinitionSummary, ProviderDefinitionsPage as ProviderDefinitionsPageDTO } from "@/lib/api-types";

import { ProviderDefinitionDetailPage } from "./ProviderDefinitionDetailPage";
import { ProvidersPage } from "./ProvidersPage";

/** renderProvidersApp mounts both the list and the full-page detail route in
 * one router tree — ProvidersPage.tsx navigates to "/providers/$slug" on
 * row-click (DESIGN.md §0#4: config-heavy surfaces are a full page) —
 * mirrors TriggerInstancesPage.test.tsx's own two-route harness shape. This
 * area is installation-wide (AC7): the harness still carries an `?org=`
 * search param (matching the shared root route shape every other feature
 * test uses) but ProvidersPage never reads it and never calls an org-scoped
 * endpoint. */
function renderProvidersApp(initialPath = "/providers/") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const listRoute = createRoute({ getParentRoute: () => rootRoute, path: "/providers/", component: ProvidersPage });
  const detailRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/providers/$slug",
    component: ProviderDefinitionDetailPage,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([listRoute, detailRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return { ...render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>), router };
}

function providerDefinition(overrides: Partial<ProviderDefinitionSummary>): ProviderDefinitionSummary {
  return {
    slug: "outlook",
    name: "Outlook",
    logo: "",
    authScheme: "oauth2",
    formatVersion: 1,
    toolCount: 2,
    triggerCount: 1,
    ...overrides,
  };
}

function mockProviderDefinitions(items: ProviderDefinitionSummary[], nextCursor?: string) {
  server.use(
    http.get("/api/v1/provider-definitions", () =>
      HttpResponse.json({ items, ...(nextCursor ? { nextCursor } : {}) } satisfies ProviderDefinitionsPageDTO),
    ),
  );
}

describe("ProvidersPage", () => {
  it("shows skeleton loading rows before the first response arrives", async () => {
    server.use(
      http.get("/api/v1/provider-definitions", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ items: [] } satisfies ProviderDefinitionsPageDTO);
      }),
    );
    renderProvidersApp();

    const table = await screen.findByRole("table");
    expect(within(table).getAllByRole("row", { hidden: true }).length).toBeGreaterThan(1);
  });

  it("shows the empty state when the installation has no loaded provider definitions", async () => {
    mockProviderDefinitions([]);
    renderProvidersApp();

    expect(await screen.findByText(/no provider definitions loaded/i)).toBeInTheDocument();
  });

  it("renders a row per provider with its name, slug chip, auth scheme, and tool/trigger counts", async () => {
    mockProviderDefinitions([providerDefinition({ slug: "outlook", name: "Outlook", authScheme: "oauth2", toolCount: 3, triggerCount: 1 })]);
    renderProvidersApp();

    await screen.findByText("Outlook");
    const table = screen.getByRole("table");
    expect(within(table).getByText("Outlook")).toBeInTheDocument();
    expect(within(table).getByRole("button", { name: /copy id outlook/i })).toBeInTheDocument();
    expect(within(table).getByText("oauth2")).toBeInTheDocument();
    expect(within(table).getByText("3")).toBeInTheDocument();
    expect(within(table).getByText("1")).toBeInTheDocument();
  });

  // AC5: a long slug renders truncated with a click-to-copy affordance — the
  // full value stays reachable (via `title`, for the copy action and screen
  // readers/browser search) even though the visible label is shortened.
  it("truncates a long provider slug in its copy chip, while keeping the full slug reachable for copying", async () => {
    const longSlug = "a-very-long-provider-slug-that-should-be-truncated";
    mockProviderDefinitions([providerDefinition({ slug: longSlug, name: "Long Slug Provider" })]);
    renderProvidersApp();

    await screen.findByText("Long Slug Provider");
    const chip = screen.getByRole("button", { name: `Copy id ${longSlug}` });
    expect(chip).toHaveAttribute("title", longSlug);
    expect(chip.textContent).not.toContain(longSlug);
    expect(chip.textContent).toContain("…");
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/provider-definitions", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The catalog is unavailable." } }, { status: 500 }),
      ),
    );
    renderProvidersApp();

    expect(await screen.findByText("The catalog is unavailable.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("fetches the next page via nextCursor when Load more is clicked, appending (not replacing) the first page's rows", async () => {
    let requestedCursor: string | null = null;
    server.use(
      http.get("/api/v1/provider-definitions", ({ request }) => {
        requestedCursor = new URL(request.url).searchParams.get("cursor");
        if (!requestedCursor) {
          return HttpResponse.json({
            items: [providerDefinition({ slug: "outlook", name: "Outlook" })],
            nextCursor: "cursor-to-page-2",
          } satisfies ProviderDefinitionsPageDTO);
        }
        return HttpResponse.json({ items: [providerDefinition({ slug: "slack", name: "Slack" })] } satisfies ProviderDefinitionsPageDTO);
      }),
    );
    renderProvidersApp();

    expect(await screen.findByText("Outlook")).toBeInTheDocument();
    screen.getByRole("button", { name: /load more/i }).click();

    await waitFor(() => expect(requestedCursor).toBe("cursor-to-page-2"));
    expect(await screen.findByText("Slack")).toBeInTheDocument();
    expect(screen.getByText("Outlook")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument());
  });

  // This slice moved "add integration" off the installation-wide Providers
  // list onto each provider's own detail page — the list surface is a pure
  // read of loaded provider definitions again.
  it("does not render a Create integration entry point on this list page", async () => {
    mockProviderDefinitions([providerDefinition({ slug: "outlook", name: "Outlook" })]);
    renderProvidersApp();

    await screen.findByText("Outlook");
    expect(screen.queryByRole("button", { name: /create integration/i })).not.toBeInTheDocument();
  });

  it("opening a row navigates to the provider's full-page bundle detail view", async () => {
    mockProviderDefinitions([providerDefinition({ slug: "outlook", name: "Outlook" })]);
    server.use(
      http.get("/api/v1/provider-definitions/outlook", () =>
        HttpResponse.json({
          slug: "outlook",
          name: "Outlook",
          formatVersion: 1,
          bundle: {
            formatVersion: 1,
            slug: "outlook",
            name: "Outlook",
            logo: "",
            authScheme: "oauth2",
            oauth: { authorizeUrl: "https://example.com/authorize" },
            mapping: { baseUrl: "https://graph.microsoft.com" },
            expectedParams: [],
            tools: [],
            triggers: [],
          },
        }),
      ),
    );
    const { router } = renderProvidersApp();

    const row = await screen.findByText("Outlook");
    row.closest("tr")?.click();

    expect(await screen.findByRole("heading", { name: "Outlook" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/providers/outlook");
  });
});
