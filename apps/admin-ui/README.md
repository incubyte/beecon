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

Phase 5 Slice 1 (PD49/PD55) replaced PD39's shared admin-key gate with real
per-operator accounts, sessions, and (Slice 3) CSRF. The SPA now holds **no
credential of any kind** in JS memory:

- `src/components/LoginScreen.tsx` posts `{ email, password }` to
  `POST /api/v1/auth/login`. On success the server sets the
  `beecon_session` (`HttpOnly`, `Secure`, `SameSite=Strict`) and
  `beecon_csrf` cookies — the browser holds them, not this app's JS.
- `src/lib/auth.ts`'s `useSession()` probes `GET /api/v1/auth/me`: 200 means
  the same-origin session cookie authenticated the request; 401 means no
  session (or an expired/revoked one). `routes/__root.tsx`'s guard reads
  this to choose `LoginScreen` vs the authenticated shell.
- `src/lib/api-client.ts` sends no `Authorization` header at all — every
  `/api/v1` call rides the same-origin session cookie automatically
  (`credentials: "same-origin"`). A `401` from any call other than the
  session probe itself invalidates the `auth.me` query, so the SPA falls
  back to `LoginScreen` on the next render.
- Sign-out (top bar, or the command palette) is wired via
  `useSignOut()` — Slice 1 only invalidates the local session probe; Slice 2
  adds the real `POST /api/v1/auth/logout` call that revokes the session and
  clears both cookies server-side (see the `TODO(Slice 2)` in
  `src/lib/auth.ts`).
- The CSRF cookie (`beecon_csrf`) is deliberately **not** `HttpOnly` — the
  SPA reads it (`readCsrfToken()`) to echo as the `X-CSRF-Token` header on
  mutating calls once Slice 3's CSRF-checking middleware lands (unused for
  now — see the `TODO(Slice 3)` in `src/lib/auth.ts` and
  `src/lib/api-client.ts`).

**Bootstrapping the first operator is deliberately not an SPA screen**
(FD-C): an admin-key-held-in-JS bootstrap form would reintroduce exactly the
posture PD55 just retired. Instead, create the first operator with a `curl`
call against the admin-key-guarded, first-account-only endpoint:

```bash
curl -X POST https://<your-beecon-host>/api/v1/operators/bootstrap \
  -H "Authorization: Bearer $BEECON_ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"a-strong-password-12-chars-plus"}'
```

This succeeds exactly once (`409` thereafter — bootstrap is first-account-
only); after that, sign in through `LoginScreen`. The admin key is retained
as a break-glass credential (see the backend architecture doc) but no longer
authenticates the console's general routes once an operator account exists.

## Development

```bash
cd apps/admin-ui
npm install
npm run dev       # Vite dev server; proxies /api to a beecon serve
                   # instance on http://localhost:8080
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
