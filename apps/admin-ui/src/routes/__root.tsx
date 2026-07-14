import { createRootRoute, Outlet } from "@tanstack/react-router";

import { AppShell } from "@/components/shell/AppShell";
import { GateScreen } from "@/components/GateScreen";
import { useIsAuthenticated } from "@/lib/auth";

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

/** The Slice 1 gate guard: no in-memory admin key -> GateScreen; a key ->
 * the authenticated shell around whichever route matched (AC2). Nothing
 * here is a router navigation — clearing or setting the key just changes
 * what this component renders on the next tick (AC5's "no navigation away",
 * and AC7's "an 401 sends the operator back to the gate"). */
function RootComponent() {
  const isAuthenticated = useIsAuthenticated();

  if (!isAuthenticated) {
    return <GateScreen />;
  }

  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
