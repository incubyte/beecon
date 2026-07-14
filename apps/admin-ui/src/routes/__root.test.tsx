import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { server } from "@/test/msw/server";
import { clearAdminKey, getAdminKey } from "@/lib/auth";

afterEach(() => {
  clearAdminKey();
});

/** renderApp mounts the real production route tree (gate guard, shell,
 * Organizations page) — this is the one test in the suite that exercises
 * __root.tsx's own gate-vs-shell branch, the composition every other test
 * file assumes rather than re-verifies. */
function renderApp(initialEntry = "/organizations") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [initialEntry] }) });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe("the admin-key gate guards the console shell (Slice 1)", () => {
  it("shows the gate screen, not the shell, when no key is set", async () => {
    renderApp();

    expect(await screen.findByRole("heading", { name: /beecon admin/i })).toBeInTheDocument();
    expect(screen.queryByRole("complementary", { name: /primary/i })).not.toBeInTheDocument();
  });

  it("mounts the shell once a key that /admin/verify accepts (204) is submitted", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));
    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });

    fireEvent.change(screen.getByLabelText(/admin key/i), { target: { value: "beecon_admin_good-key" } });
    fireEvent.click(screen.getByRole("button", { name: /open console/i }));

    // The shell's own left sidebar (aria-label "Primary", Sidebar.tsx) is
    // the signal the gate was replaced, not just that the gate disappeared.
    expect(await screen.findByRole("complementary", { name: /primary/i })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /beecon admin/i })).not.toBeInTheDocument();
    expect(getAdminKey()).toBe("beecon_admin_good-key");
  });

  it("stays on the gate screen (no shell) when the key is rejected (401)", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 401 })));
    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });

    fireEvent.change(screen.getByLabelText(/admin key/i), { target: { value: "beecon_admin_bad-key" } });
    fireEvent.click(screen.getByRole("button", { name: /open console/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/rejected/i);
    expect(screen.queryByRole("complementary", { name: /primary/i })).not.toBeInTheDocument();
    expect(getAdminKey()).toBeNull();
  });

  it("sign-out (top bar) clears the key and returns to the gate screen", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));
    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });
    fireEvent.change(screen.getByLabelText(/admin key/i), { target: { value: "beecon_admin_good-key" } });
    fireEvent.click(screen.getByRole("button", { name: /open console/i }));
    await screen.findByRole("complementary", { name: /primary/i });

    fireEvent.click(screen.getByRole("button", { name: /sign out/i }));

    expect(await screen.findByRole("heading", { name: /beecon admin/i })).toBeInTheDocument();
    expect(getAdminKey()).toBeNull();
  });

  // AC7 ("an API call that returns 401 sends the operator back to the
  // gate") is the real store's onUnauthorized wiring, already pinned
  // end-to-end in api-client.test.ts ("a client wired with the real auth
  // store clears the in-memory admin key on a 401"); RootComponent's
  // gate-vs-shell branch reacting to that same store (rather than a
  // separate concept) is exactly what "mounts the shell" /
  // "stays on the gate screen" above already exercise via useIsAuthenticated.
});
