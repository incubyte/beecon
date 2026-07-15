import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { server } from "@/test/msw/server";

/**
 * renderApp mounts the real route tree (the session guard in __root.tsx,
 * LoginScreen, and the authenticated AppShell) — the same "boot through the
 * real router" convention routes/index.test.tsx's own renderAppAuthenticated
 * helper establishes, kept local to this file since each test here drives a
 * different session state rather than always logging in first.
 */
function renderApp(initialEntry = "/organizations") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [initialEntry] }) });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

describe("the root session guard (Phase 5 Slice 1)", () => {
  it("shows the login screen when GET /auth/me returns 401 (the MSW default: no session)", async () => {
    renderApp();

    expect(await screen.findByRole("heading", { name: /beecon admin/i })).toBeInTheDocument();
    expect(screen.queryByRole("complementary", { name: /primary/i })).not.toBeInTheDocument();
  });

  it("mounts the authenticated shell when GET /auth/me returns 200", async () => {
    server.use(http.get("/api/v1/auth/me", () => HttpResponse.json({ id: "op_1", email: "operator@example.com" })));

    renderApp();

    expect(await screen.findByRole("complementary", { name: /primary/i })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: /beecon admin/i })).not.toBeInTheDocument();
  });

  it("renders neither the login screen nor the shell while the session probe is still pending", () => {
    server.use(http.get("/api/v1/auth/me", () => new Promise(() => {}))); // never resolves

    renderApp();

    expect(screen.queryByRole("heading", { name: /beecon admin/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("complementary", { name: /primary/i })).not.toBeInTheDocument();
  });

  it("logging in through LoginScreen transitions from the login screen to the shell without a page navigation", async () => {
    let loggedIn = false;
    server.use(
      http.post("/api/v1/auth/login", () => {
        loggedIn = true;
        return new HttpResponse(null, { status: 204 });
      }),
      http.get("/api/v1/auth/me", () =>
        loggedIn
          ? HttpResponse.json({ id: "op_1", email: "operator@example.com" })
          : new HttpResponse(null, { status: 401 }),
      ),
    );

    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });
    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "operator@example.com" } });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByRole("complementary", { name: /primary/i })).toBeInTheDocument();
  });

  it("never sends an Authorization header for the session probe — the beecon_session cookie authenticates it instead", async () => {
    let capturedAuthHeader: string | null = "not-yet-captured";
    server.use(
      http.get("/api/v1/auth/me", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return new HttpResponse(null, { status: 401 });
      }),
    );

    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });

    expect(capturedAuthHeader).toBeNull();
  });
});
