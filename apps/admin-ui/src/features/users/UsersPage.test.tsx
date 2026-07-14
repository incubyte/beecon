import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { EndUser, UsersPage as UsersPageDTO } from "@/lib/api-types";

import { UsersPage } from "./UsersPage";

/** renderUsersPage mirrors OrganizationsPage.test.tsx/ConnectionsPage.test.tsx's
 * own harness: UsersPage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderUsersPage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: UsersPage });
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

function user(overrides: Partial<EndUser> = {}): EndUser {
  return {
    id: "user_1",
    name: "Ada Lovelace",
    externalId: "",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockOrgScopedUsers(orgId: string, items: EndUser[], nextCursor?: string) {
  server.use(
    http.get(`/api/v1/organizations/${orgId}/users`, () =>
      HttpResponse.json({ items, ...(nextCursor ? { nextCursor } : {}) } satisfies UsersPageDTO),
    ),
  );
}

describe("UsersPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/users", () => {
        requested = true;
        return HttpResponse.json({ items: [] } satisfies UsersPageDTO);
      }),
    );
    renderUsersPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows the empty state when the org has no end-users", async () => {
    mockOrgScopedUsers("org_1", []);
    renderUsersPage();

    expect(await screen.findByText(/no end-users yet/i)).toBeInTheDocument();
  });

  it("lists end-users from the org-scoped GET, showing name and externalId", async () => {
    mockOrgScopedUsers("org_1", [user({ id: "user_1", name: "Ada Lovelace", externalId: "ext-1" })]);
    renderUsersPage();

    expect(await screen.findByText("Ada Lovelace")).toBeInTheDocument();
    expect(screen.getByText("ext-1")).toBeInTheDocument();
  });

  it("creates a user via CreateUserModal and refreshes the list", async () => {
    mockOrgScopedUsers("org_1", []);
    let createdBody: unknown;
    server.use(
      http.post("/api/v1/organizations/org_1/users", async ({ request }) => {
        createdBody = await request.json();
        // After creation, the invalidated list refetch should include the
        // new user — this handler stays registered for the refetch too.
        return HttpResponse.json(user({ id: "user_new", name: "Grace Hopper", externalId: "" }), { status: 201 });
      }),
    );
    renderUsersPage();
    await screen.findByText(/no end-users yet/i);
    mockOrgScopedUsers("org_1", [user({ id: "user_new", name: "Grace Hopper" })]);

    fireEvent.click(screen.getByRole("button", { name: /create user/i }));
    fireEvent.change(await screen.findByLabelText(/^name$/i), { target: { value: "Grace Hopper" } });
    fireEvent.click(screen.getByRole("button", { name: /^create user$/i }));

    await waitFor(() => expect(createdBody).toEqual({ name: "Grace Hopper", externalId: "" }));
    expect(await screen.findByText("Grace Hopper")).toBeInTheDocument();
  });

  it("re-scopes to the newly selected org when ?org= changes, never showing the previous org's users", async () => {
    mockOrgScopedUsers("org_a", [user({ id: "user_org_a", name: "Org A's User" })]);
    mockOrgScopedUsers("org_b", [user({ id: "user_org_b", name: "Org B's User" })]);

    const { router } = renderUsersPage("/?org=org_a");

    await screen.findByText("Org A's User");
    expect(screen.queryByText("Org B's User")).not.toBeInTheDocument();

    await router.navigate({ to: "/", search: { org: "org_b" } });

    await screen.findByText("Org B's User");
    expect(screen.queryByText("Org A's User")).not.toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the request fails", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/users", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The users list could not be loaded." } }, { status: 500 }),
      ),
    );
    renderUsersPage();

    expect(await screen.findByText("The users list could not be loaded.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});
