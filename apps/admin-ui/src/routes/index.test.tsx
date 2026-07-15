import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { server } from "@/test/msw/server";

/** renderAppAuthenticated mirrors the production route tree (session guard,
 * shell, Organizations page), logging in through the real login flow first
 * — the redirect this file tests only ever matters once the shell has
 * mounted. */
async function renderAppAuthenticated(initialEntry: string) {
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
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/organizations"] }) });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await screen.findByRole("heading", { name: /beecon admin/i });
  fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "operator@example.com" } });
  fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "correct-password" } });
  fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
  await screen.findByRole("complementary", { name: /primary/i });

  await router.navigate({ to: initialEntry });
}

describe("the root index route (Slice 3)", () => {
  it("redirects '/' to '/dashboard' — the dashboard is the default post-login landing", async () => {
    await renderAppAuthenticated("/");

    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
  });
});
