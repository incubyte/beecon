import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { Governance, IntegrationVisibility } from "@/lib/api-types";

import { GovernancePage } from "./GovernancePage";

/** renderGovernancePage mirrors UsersPage.test.tsx/ConnectionsPage.test.tsx's
 * own harness: GovernancePage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderGovernancePage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: GovernancePage });
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

function governance(overrides: Partial<Governance> = {}): Governance {
  return {
    allowList: null,
    hidden: [],
    onboarding: { featured: [], cap: 8 },
    ...overrides,
  };
}

function integration(overrides: Partial<IntegrationVisibility> = {}): IntegrationVisibility {
  return {
    id: "intg_1",
    providerSlug: "outlook",
    name: "Outlook",
    logo: "",
    authScheme: "oauth2",
    visibility: "VISIBLE",
    ...overrides,
  };
}

function mockGovernance(orgId: string, gov: Governance) {
  server.use(http.get(`/api/v1/organizations/${orgId}/governance`, () => HttpResponse.json(gov)));
}

function mockCatalog(orgId: string, items: IntegrationVisibility[]) {
  server.use(http.get(`/api/v1/organizations/${orgId}/governance/catalog`, () => HttpResponse.json(items)));
}

/** waitForLoaded resolves once the "Allow-list" tab (the default active tab)
 * is on screen — a single retrying query rather than a separate
 * "loading gone" waitFor followed by an immediate getBy, which races the
 * governance query, the catalog query, and useGovernanceForm's own
 * seed-on-data-arrival effect settling across more than one render. */
async function waitForLoaded() {
  return screen.findByRole("tab", { name: /allow-list/i });
}

/** selectTab switches Radix Tabs to the panel named by name. Radix's Tabs
 * use "automatic" activation (the standard ARIA tabs pattern): a trigger
 * activates on receiving FOCUS, not merely on a synthetic click event —
 * jsdom's fireEvent.click does not itself move focus the way a real mouse
 * click does, so this explicitly focuses the trigger first. */
function selectTab(name: RegExp) {
  const tab = screen.getByRole("tab", { name });
  tab.focus();
  fireEvent.click(tab);
}

describe("GovernancePage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/governance", () => {
        requested = true;
        return HttpResponse.json(governance());
      }),
    );
    renderGovernancePage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("loads the org's governance and catalog, rendering progressive-disclosure tabs for allow-list, visibility, and onboarding", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration()]);
    renderGovernancePage();

    await waitForLoaded();

    expect(screen.getByRole("tab", { name: /allow-list/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /^visibility$/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /onboarding/i })).toBeInTheDocument();
    // The Save action bar stays visible regardless of the active tab (AC9).
    expect(screen.getByRole("button", { name: /save changes/i })).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the governance request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/governance", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Governance could not be loaded." } }, { status: 500 }),
      ),
      http.get("/api/v1/organizations/org_1/governance/catalog", () => HttpResponse.json([])),
    );
    renderGovernancePage();

    expect(await screen.findByText("Governance could not be loaded.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("re-scopes to the newly selected org when ?org= changes, never mixing the previous org's governance", async () => {
    mockGovernance("org_a", governance({ allowList: ["intg_a_only"] }));
    mockCatalog("org_a", [integration({ id: "intg_a_only", name: "Org A's integration" })]);
    mockGovernance("org_b", governance({ allowList: null }));
    mockCatalog("org_b", [integration({ id: "intg_b_only", name: "Org B's integration" })]);

    const { router } = renderGovernancePage("/?org=org_a");
    await waitForLoaded();
    // The Visibility tab always lists every catalog integration regardless
    // of allow-list state (unlike the Allow-list tab, which only shows the
    // per-integration list once the allow-list switch is on) — the reliable
    // place to observe which org's catalog is currently loaded.
    selectTab(/^visibility$/i);

    await screen.findByText("Org A's integration");
    expect(screen.queryByText("Org B's integration")).not.toBeInTheDocument();

    await router.navigate({ to: "/", search: { org: "org_b" } });
    // Switching org re-triggers the loading state (a fresh query key), which
    // unmounts and remounts the tab group — re-select Visibility once org
    // B's data has loaded.
    await waitForLoaded();
    selectTab(/^visibility$/i);

    await screen.findByText("Org B's integration");
    expect(screen.queryByText("Org A's integration")).not.toBeInTheDocument();
  });

  it("toggling the allow-list switch on reveals the per-integration checkbox list", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration({ id: "intg_1", name: "Outlook" })]);
    renderGovernancePage();
    await waitForLoaded();

    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("switch", { name: /restrict this organization to an allow-list/i }));

    expect(await screen.findByRole("checkbox")).toBeInTheDocument();
    expect(screen.getByText("Outlook")).toBeInTheDocument();
  });

  it("saves the exact PUT payload shape the backend accepts when an allow-list is enabled and an integration is checked", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration({ id: "intg_1", name: "Outlook" })]);
    let putBody: unknown;
    server.use(
      http.put("/api/v1/organizations/org_1/governance", async ({ request }) => {
        putBody = await request.json();
        return HttpResponse.json(governance({ allowList: ["intg_1"] }));
      }),
    );
    renderGovernancePage();
    await waitForLoaded();

    fireEvent.click(screen.getByRole("switch", { name: /restrict this organization to an allow-list/i }));
    fireEvent.click(await screen.findByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() =>
      expect(putBody).toEqual({
        allowList: ["intg_1"],
        hidden: [],
        onboarding: { featured: [], cap: 8 },
      }),
    );
  });

  it("saves allowList: null when the allow-list switch stays off (inherit the full catalog)", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration()]);
    let putBody: unknown;
    server.use(
      http.put("/api/v1/organizations/org_1/governance", async ({ request }) => {
        putBody = await request.json();
        return HttpResponse.json(governance());
      }),
    );
    renderGovernancePage();
    await waitForLoaded();

    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(putBody).toMatchObject({ allowList: null }));
  });

  it("hiding an integration on the Visibility tab and saving PUTs it in the hidden array", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration({ id: "intg_1", name: "Outlook" })]);
    let putBody: { hidden?: string[] } | undefined;
    server.use(
      http.put("/api/v1/organizations/org_1/governance", async ({ request }) => {
        putBody = (await request.json()) as { hidden?: string[] };
        return HttpResponse.json(governance({ hidden: ["intg_1"] }));
      }),
    );
    renderGovernancePage();
    await waitForLoaded();

    selectTab(/^visibility$/i);
    fireEvent.click(await screen.findByRole("switch", { name: /hide outlook for this organization/i }));
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(putBody?.hidden).toEqual(["intg_1"]));
  });

  it("the Visibility tab shows each integration's effective visibility as a color+icon+text badge, not color alone", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [
      integration({ id: "intg_visible", name: "Visible One", visibility: "VISIBLE" }),
      integration({ id: "intg_hidden", name: "Hidden One", visibility: "HIDDEN" }),
      integration({ id: "intg_not_allowed", name: "Not Allowed One", visibility: "NOT_ALLOWED" }),
    ]);
    renderGovernancePage();
    await waitForLoaded();

    selectTab(/^visibility$/i);
    await screen.findByText("Visible One");

    // Query each row by its integration name, then assert on the badge
    // scoped to that row — the table also has a "Hidden" column header
    // whose own exact text would otherwise collide with the HIDDEN badge's
    // label.
    function rowBadgeText(integrationName: string): string {
      const row = screen.getByText(integrationName).closest("tr");
      if (!row) throw new Error(`no table row found for ${integrationName}`);
      return within(row).getByText(/^(visible|hidden|not allowed)$/i).textContent ?? "";
    }

    expect(rowBadgeText("Visible One")).toBe("Visible");
    expect(rowBadgeText("Hidden One")).toBe("Hidden");
    expect(rowBadgeText("Not Allowed One")).toBe("Not allowed");
  });

  it("the Onboarding tab disables adding another featured integration once the cap is reached", async () => {
    mockGovernance("org_1", governance({ onboarding: { featured: ["intg_1"], cap: 1 } }));
    mockCatalog("org_1", [
      integration({ id: "intg_1", name: "Outlook" }),
      integration({ id: "intg_2", name: "Slack" }),
    ]);
    renderGovernancePage();
    await waitForLoaded();

    selectTab(/onboarding/i);

    const select = await screen.findByRole("combobox", { name: /add to featured/i });
    expect(select).toBeDisabled();
    expect(within(select).getByText(/featured cap reached/i)).toBeInTheDocument();
  });

  it("the Onboarding tab's featured cap round-trips through the PUT payload", async () => {
    mockGovernance("org_1", governance({ onboarding: { featured: ["intg_1"], cap: 8 } }));
    mockCatalog("org_1", [integration({ id: "intg_1", name: "Outlook" })]);
    let putBody: { onboarding?: { cap?: number } } | undefined;
    server.use(
      http.put("/api/v1/organizations/org_1/governance", async ({ request }) => {
        putBody = (await request.json()) as { onboarding?: { cap?: number } };
        return HttpResponse.json(governance());
      }),
    );
    renderGovernancePage();
    await waitForLoaded();

    selectTab(/onboarding/i);
    fireEvent.change(await screen.findByLabelText(/featured cap/i), { target: { value: "3" } });
    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    await waitFor(() => expect(putBody?.onboarding?.cap).toBe(3));
  });

  it("shows an inline error message in the save bar and never navigates away when saving fails", async () => {
    mockGovernance("org_1", governance());
    mockCatalog("org_1", [integration()]);
    server.use(
      http.put("/api/v1/organizations/org_1/governance", () =>
        HttpResponse.json({ error: { code: "validation_failed", message: "Featured list exceeds the cap." } }, { status: 422 }),
      ),
    );
    renderGovernancePage();
    await waitForLoaded();

    fireEvent.click(screen.getByRole("button", { name: /save changes/i }));

    expect(await screen.findByText("Featured list exceeds the cap.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save changes/i })).toBeInTheDocument();
  });
});
