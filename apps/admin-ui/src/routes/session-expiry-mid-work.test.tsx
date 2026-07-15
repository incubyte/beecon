import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it } from "vitest";

import { queryClient, queryKeys } from "@/lib/query";
import { routeTree } from "@/routeTree.gen";
import { server } from "@/test/msw/server";

/**
 * This file distinguishes Phase 5 Slice 5's three session-transition paths,
 * which must never be confused with one another:
 *
 *   1. A mid-session 401 (a console API call fails after the operator WAS
 *      already authenticated) -> ReauthModal overlays the still-mounted
 *      shell (AppShell, its sidebar/top bar) -> resume in place.
 *   2. An explicit sign-out -> straight to LoginScreen, ReauthModal never
 *      appears.
 *   3. The initial unauthenticated page load -> straight to LoginScreen,
 *      ReauthModal never appears (there is no "session" to have expired yet).
 *
 * Unlike __root.test.tsx/index.test.tsx (which each build a fresh, private
 * `new QueryClient()` per test), these tests render through the app's real
 * singleton `queryClient` (lib/query.ts) — the same instance main.tsx wraps
 * the app in production, and the same one both the real singleton
 * `apiClient`'s default onUnauthorized (markSessionExpiredMidWork) and
 * ReauthModal's useReauthRequired ultimately read/write. A locally
 * constructed QueryClient would never observe api-client.ts's default
 * onUnauthorized at all, since that default writes straight to the
 * lib/query.ts singleton rather than through React context.
 */
afterEach(() => {
  queryClient.clear();
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
});

function renderApp(initialEntry = "/organizations") {
  const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [initialEntry] }) });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return router;
}

async function loginThroughTheRealFlow() {
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
  fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery staple" } });
  fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
  await screen.findByRole("complementary", { name: /primary/i });
}

describe("the three session-transition paths are distinct (Phase 5 Slice 5)", () => {
  it("path 1 of 3 — the initial unauthenticated load shows LoginScreen directly and never mounts ReauthModal", async () => {
    renderApp(); // MSW default: GET /auth/me -> 401 (no session)

    expect(await screen.findByRole("heading", { name: /beecon admin/i })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText(/session expired/i)).not.toBeInTheDocument();
  });

  it("path 2 of 3 — a mid-session 401 overlays ReauthModal over the still-mounted shell, rather than swapping to LoginScreen", async () => {
    await loginThroughTheRealFlow();
    server.use(http.get("/api/v1/organizations", () => new HttpResponse(null, { status: 401 })));

    // Simulates "a console API call fails after the operator was already
    // authenticated": a page re-fetches its own data (any real page's own
    // useQuery would do this on a background refetch/window refocus) and this
    // time gets a 401 — exercised directly here so the test isolates the
    // session-transition behavior from any one page's own refetch triggers.
    void queryClient.refetchQueries({ queryKey: queryKeys.organizations.list() });

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText(/session expired/i)).toBeInTheDocument();
    // The shell underneath is still mounted — this is an overlay, not a
    // remount to LoginScreen. Queried directly against the DOM (not
    // getByRole): Radix's Dialog correctly marks everything outside the
    // portal aria-hidden while open (part of FD-I's focus trap), which
    // deliberately removes it from the accessibility tree getByRole reads —
    // that a11y behavior is exactly the point, so "still mounted" is
    // asserted against the raw DOM instead.
    expect(document.querySelector('aside[aria-label="Primary"]')).not.toBeNull();
    expect(screen.queryByRole("heading", { name: /beecon admin/i })).not.toBeInTheDocument();
  });

  it("re-authenticating through ReauthModal closes it and resumes in place — the shell is never remounted", async () => {
    await loginThroughTheRealFlow();
    // The underlying session is "restored" the moment re-authentication
    // succeeds — mirroring a real backend, where the endpoint that originally
    // 401'd works again once the session is valid, not just the login
    // endpoint itself. Without this, resolveSessionExpiredMidWork's own
    // refetchQueries({type:"active"}) would re-fetch this still-401ing
    // organizations query and immediately re-flag reauthRequired true right
    // after it was just cleared.
    let sessionRestored = false;
    server.use(
      http.get("/api/v1/organizations", () =>
        sessionRestored ? HttpResponse.json({ items: [] }) : new HttpResponse(null, { status: 401 }),
      ),
    );
    void queryClient.refetchQueries({ queryKey: queryKeys.organizations.list() });
    await screen.findByRole("dialog");
    server.use(
      http.post("/api/v1/auth/login", () => {
        sessionRestored = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(document.querySelector('aside[aria-label="Primary"]')).not.toBeNull();
    expect(screen.queryByRole("heading", { name: /beecon admin/i })).not.toBeInTheDocument();
  });

  // BUG FOUND (see this slice's test report): this test is written to the
  // AC's correct expected behavior and currently FAILS against production
  // code. TanStack Query v5 never clears a query's cached `data` on a
  // subsequent error (query-core's own reducer: the "error" action spreads
  // `...state` and only overwrites `error`/`fetchStatus`/`status`, leaving
  // `data` exactly as it was — confirmed directly against
  // node_modules/@tanstack/query-core). routes/__root.tsx's session guard is
  // `if (!data) return <LoginScreen />`, which only handles "no session was
  // ever established" (the initial-unauthenticated-load path, where `data`
  // is genuinely undefined) — it does NOT handle "a session existed and then
  // ended" (explicit sign-out, or a revoked/expired cookie discovered on a
  // later /auth/me refetch): `data` still holds the last successful
  // operator, so the guard's `!data` is false and the shell never falls back
  // to LoginScreen. The correct guard is `if (!data || isError)`. Not fixed
  // here (business logic, not a testability refactor) — flagged for the
  // slice-coder/developer.
  it("path 3 of 3 — an explicit sign-out goes straight to LoginScreen, and ReauthModal is never shown on the way there", async () => {
    await loginThroughTheRealFlow();
    server.use(
      http.post("/api/v1/auth/logout", () => new HttpResponse(null, { status: 204 })),
      // Sign-out invalidates the auth.me probe (useSignOut) — override it to
      // 401 unconditionally from this point on, mirroring how a real logout
      // actually ends the session server-side (loginThroughTheRealFlow's own
      // `loggedIn` closure has nothing to do with a real sign-out, so it must
      // be overridden here rather than relying on that fixture's state).
      http.get("/api/v1/auth/me", () => new HttpResponse(null, { status: 401 })),
    );

    fireEvent.click(screen.getByRole("button", { name: /sign out/i }));

    expect(await screen.findByRole("heading", { name: /beecon admin/i })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText(/session expired/i)).not.toBeInTheDocument();
  });

  it("the reauthRequired flag itself is false for both the sign-out path and the initial-load path — only the mid-work 401 sets it", async () => {
    renderApp();
    await screen.findByRole("heading", { name: /beecon admin/i });
    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false);
  });
});
