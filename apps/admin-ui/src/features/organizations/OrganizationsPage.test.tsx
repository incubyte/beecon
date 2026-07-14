import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { Organization, OrganizationsPage as OrganizationsPageDTO } from "@/lib/api-types";

import { OrganizationsPage } from "./OrganizationsPage";

/** renderOrganizationsPage mounts OrganizationsPage as the sole routed
 * component behind a real (but minimal) TanStack Router + Query context —
 * useNavigate (the row-click org-scoping action) needs a real router, but
 * the shell (sidebar/top bar) is deliberately not part of this harness: a
 * feature test should fail only for reasons inside the feature itself. */
function renderOrganizationsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: OrganizationsPage });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function org(overrides: Partial<Organization>): Organization {
  return {
    id: "org_1",
    name: "Acme",
    allowedRedirectUris: [],
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

describe("OrganizationsPage", () => {
  it("shows skeleton loading rows before the first response arrives", async () => {
    server.use(
      http.get("/api/v1/organizations", async () => {
        await new Promise((resolve) => setTimeout(resolve, 20));
        return HttpResponse.json({ items: [] } satisfies OrganizationsPageDTO);
      }),
    );
    renderOrganizationsPage();

    const table = await screen.findByRole("table");
    // SkeletonRows marks its placeholder rows aria-hidden — they exist
    // in the DOM as soon as isLoading is true, before any real row lands.
    expect(within(table).getAllByRole("row", { hidden: true }).length).toBeGreaterThan(1);
  });

  it("shows the empty state when the installation has no organizations", async () => {
    server.use(http.get("/api/v1/organizations", () => HttpResponse.json({ items: [] } satisfies OrganizationsPageDTO)));
    renderOrganizationsPage();

    expect(await screen.findByText(/no organizations yet/i)).toBeInTheDocument();
  });

  it("renders a row per organization with a mono copy-id chip and the created date", async () => {
    server.use(
      http.get("/api/v1/organizations", () =>
        HttpResponse.json({
          items: [org({ id: "org_abcdefghij1234", name: "Acme", createdAt: "2026-03-15T00:00:00.000Z" })],
        } satisfies OrganizationsPageDTO),
      ),
    );
    renderOrganizationsPage();

    expect(await screen.findByText("Acme")).toBeInTheDocument();
    const copyButton = screen.getByRole("button", { name: /copy id org_abcdefghij1234/i });
    expect(copyButton).toHaveClass("font-mono", { exact: false });
    // The exact created-date rendering is locale-dependent ("Mar 15, 2026"
    // vs "15 Mar 2026" have both been observed); accept either ordering
    // rather than assume one.
    expect(screen.getByText(/(mar 15, 2026|15 mar 2026)/i)).toBeInTheDocument();
  });

  it("shows an inline error card (the PD5 error's own message) with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Something went wrong upstream." } }, { status: 500 }),
      ),
    );
    renderOrganizationsPage();

    expect(await screen.findByText("Something went wrong upstream.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  it("falls back to a generic message when the error envelope carries an empty one", async () => {
    server.use(
      http.get("/api/v1/organizations", () => HttpResponse.json({ error: { code: "internal_error", message: "" } }, { status: 500 })),
    );
    renderOrganizationsPage();

    expect(await screen.findByText(/could not be loaded/i)).toBeInTheDocument();
  });

  it("fetches the next page via nextCursor when Load more is clicked, appending (not replacing) the first page's rows", async () => {
    let requestedCursor: string | null = null;
    server.use(
      http.get("/api/v1/organizations", ({ request }) => {
        requestedCursor = new URL(request.url).searchParams.get("cursor");
        if (!requestedCursor) {
          return HttpResponse.json({
            items: [org({ id: "org_1", name: "First" })],
            nextCursor: "cursor-to-page-2",
          } satisfies OrganizationsPageDTO);
        }
        return HttpResponse.json({ items: [org({ id: "org_2", name: "Second" })] } satisfies OrganizationsPageDTO);
      }),
    );
    renderOrganizationsPage();

    expect(await screen.findByText("First")).toBeInTheDocument();
    const loadMoreButton = screen.getByRole("button", { name: /load more/i });

    loadMoreButton.click();

    await waitFor(() => expect(requestedCursor).toBe("cursor-to-page-2"));
    expect(await screen.findByText("Second")).toBeInTheDocument();
    // The first page's row is still there — Load more appends, it doesn't
    // replace the accumulated list.
    expect(screen.getByText("First")).toBeInTheDocument();
    // No third page was advertised, so Load more disappears.
    await waitFor(() => expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument());
  });

  it("does not show a Load more button when the page has no nextCursor", async () => {
    server.use(
      http.get("/api/v1/organizations", () =>
        HttpResponse.json({ items: [org({ id: "org_1", name: "Only One" })] } satisfies OrganizationsPageDTO),
      ),
    );
    renderOrganizationsPage();

    await screen.findByText("Only One");
    expect(screen.queryByRole("button", { name: /load more/i })).not.toBeInTheDocument();
  });
});
