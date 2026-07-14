# Beecon Admin UI

An operator console for a self-hosted Beecon installation: organizations,
connections, trigger instances, logs, events/delivery, the catalog, users,
API keys, governance, and settings. Built to static assets and embedded in
the `beecon serve` Go binary under `/admin` (PD47) — there is still exactly
one artifact to ship; this app has no server of its own.

## Stack

Plain Vite + [TanStack Router](https://tanstack.com/router) (file-based
routes, `basepath: '/admin'`), [TanStack Query](https://tanstack.com/query)
for all server state, [TanStack Table](https://tanstack.com/table) for
headless table logic, [Radix UI Primitives](https://www.radix-ui.com/) +
[cmdk](https://cmdk.paco.me/) for accessible behavior, and Tailwind CSS v4
for styling — every color/radius/shadow/spacing value is a CSS custom
property from `src/styles/tokens.css`, consumed as a Tailwind theme token
(`src/styles/globals.css`'s `@theme` block), never a hardcoded hex. Fonts
are self-hosted via `@fontsource` (IBM Plex Sans/Mono) — no runtime CDN.

**Note on `@tanstack/react-start`:** the architecture doc names TanStack
Start as the framework, run in SPA mode (SSR/server-functions off). This
app uses TanStack Start's own routing/build foundation — Vite +
`@tanstack/react-router` + the router's Vite plugin — directly, rather than
the `@tanstack/react-start` package itself, which is built for a
Nitro-hosted SSR/server-functions deployment this project deliberately
never uses (the Go binary is the only server, PD47). The result is
functionally identical to Start with SSR disabled: a client-only SPA, file-
based routes, one Vite build. Revisit if a future slice needs something
Start's package uniquely provides.

## Auth — read this before touching `src/lib/auth.ts`

Real operator accounts, sessions, and CSRF are explicitly deferred (PD39).
This console authenticates every request with the installation's single,
shared `BEECON_ADMIN_API_KEY`, entered once on the gate screen
(`src/components/GateScreen.tsx`) and held in a plain JS module variable
(`src/lib/auth.ts`) for the browser tab's lifetime only:

- **Never** written to `localStorage`, `sessionStorage`, or a cookie.
- Gone on reload or in a new tab — the gate screen shows again.
- Sent as `Authorization: Bearer <key>` on every `/api/v1` call
  (`src/lib/api-client.ts`); a `401` response (wrong key, or an
  already-open session whose key stopped working) clears it and the gate
  reappears automatically.
- Sign-out (top bar, or the command palette) clears it the same way.

This is a deliberate, documented trade-off: a single shared credential with
no per-operator identity or audit trail, intended for a trusted-operator,
network-restricted deployment (self-hosted behind a VPN / reverse-proxy
auth) — not a substitute for real authentication. Do not "fix" this by
adding persistence; that would silently widen the credential's exposure
window past what PD39 accepted. Real accounts/sessions/CSRF/SSO are a later
phase (the gate's layout already leaves the SSO slot empty for it).

## Development

```bash
cd apps/admin-ui
npm install
npm run dev       # Vite dev server; proxies /api and /admin/verify to
                   # a beecon serve instance on http://localhost:8080
```

## Building for the Go binary

```bash
npm run build      # tsc -b && vite build -> ../../server/internal/adminui/dist
```

`vite build`'s `outDir` points directly at the Go adapter's embed target
(`server/internal/adminui/dist`), matching FD2. From the repo root,
`make build-ui` runs this before `go build ./...` embeds the result — see
the repo root `Makefile` (`build-ui`/`build`/`run` targets) for the combined
pipeline. That directory is git-ignored except a placeholder `index.html` +
`.gitkeep` committed so `//go:embed dist` compiles on a clean checkout that
hasn't run a UI build yet; never commit the real build output.

## Testing

```bash
npm run test        # vitest run
npm run typecheck   # tsc -b --noEmit
```

Component/unit tests use Vitest + Testing Library + jsdom; MSW mocks
`/api/v1` so hooks/components exercise realistic PD5 error envelopes,
cursor pages, and 401s without a live backend.
