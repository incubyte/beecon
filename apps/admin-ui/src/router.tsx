import { createRouter } from "@tanstack/react-router";

import { routeTree } from "./routeTree.gen";

/**
 * basepath '/admin' (architecture doc §2.1): every route and every
 * generated link resolves under /admin, matching the Go binary's embedded
 * mount (PD47) — so the SPA works identically served under that prefix or
 * (in dev) proxied through Vite.
 */
export const router = createRouter({
  routeTree,
  basepath: "/admin",
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
