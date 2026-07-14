import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { routeTree } from "@/routeTree.gen";
import { server } from "@/test/msw/server";
import { clearAdminKey } from "@/lib/auth";

afterEach(() => {
  clearAdminKey();
});

/** renderAppAuthenticated mirrors __root.test.tsx's own renderApp, but logs
 * in through the real gate flow first — the redirect this file tests only
 * ever matters once the shell has mounted. */
async function renderAppAuthenticated(initialEntry: string) {
  server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/organizations"] }) });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await screen.findByRole("heading", { name: /beecon admin/i });
  fireEvent.change(screen.getByLabelText(/admin key/i), { target: { value: "beecon_admin_good-key" } });
  fireEvent.click(screen.getByRole("button", { name: /open console/i }));
  await screen.findByRole("complementary", { name: /primary/i });

  await router.navigate({ to: initialEntry });
}

describe("the root index route (Slice 3)", () => {
  it("redirects '/' to '/dashboard' — the dashboard is the default post-login landing", async () => {
    await renderAppAuthenticated("/");

    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument();
  });
});
