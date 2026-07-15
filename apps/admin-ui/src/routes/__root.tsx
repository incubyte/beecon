import { createRootRoute, Outlet } from "@tanstack/react-router";

import { AppShell } from "@/components/shell/AppShell";
import { LoginScreen } from "@/components/LoginScreen";
import { useSession } from "@/lib/auth";

/** RootSearch is the org-scoping search param every route shares (§2.4): the
 * top-bar org switcher and the Organizations list both write it; org-scoped
 * views (Slice 2+) read it. */
export interface RootSearch {
  org?: string;
}

function parseOrgSearch(search: Record<string, unknown>): RootSearch {
  return { org: typeof search.org === "string" ? search.org : undefined };
}

export const Route = createRootRoute({
  validateSearch: parseOrgSearch,
  component: RootComponent,
});

/**
 * The Phase 5 Slice 1 session guard (PD49/PD55; Slice 5 fix): while the GET
 * /api/v1/auth/me probe is in flight, render nothing rather than flashing
 * LoginScreen before the answer is known; otherwise LoginScreen unless the
 * probe both has data AND did not error.
 *
 * `isError` (not just `!data`) matters because TanStack Query v5 never
 * clears a query's cached `data` on a subsequent error — its "error" action
 * spreads `...state` and only touches `error`/`fetchStatus`/`status`. So
 * once `/auth/me` has succeeded once, a later refetch that 401s (an explicit
 * sign-out's own auth.me invalidation, or a revoked/expired cookie
 * discovered whenever this query happens to refetch) would leave `data`
 * still holding the previous operator forever — `!data` alone would never
 * be true again, and the shell would never fall back to LoginScreen despite
 * the operator having just signed out.
 *
 * This is deliberately a check on THIS query's own error state, not on
 * lib/session-state.ts's `reauthRequired` flag: a mid-session 401 from some
 * OTHER console call (Slice 5's ReauthModal path) never touches `/auth/me`
 * at all (api-client.ts's onUnauthorized explicitly excludes that path), so
 * `isError` here stays false throughout that path and this guard never
 * fires — AppShell stays mounted and ReauthModal overlays it, instead of
 * this guard swapping to LoginScreen underneath. The three session
 * transitions stay distinct: initial unauthenticated load and explicit
 * sign-out both resolve through this query's own pending/error/data state
 * straight to LoginScreen; a mid-work 401 on some other call resolves
 * through the separate reauthRequired flag to ReauthModal, never through
 * here.
 */
function RootComponent() {
  const { data, isPending, isError } = useSession();

  if (isPending) {
    return null;
  }

  if (!data || isError) {
    return <LoginScreen />;
  }

  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
