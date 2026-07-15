# Spec: Beecon Phase 5 (operator-auth sub-phase) — Real Operator Authentication

> This is the **lead sub-phase of Phase 5** (developer decision, 2026-07-15: the registry
> and `tool_` ids are deferred — see the deferred
> [`beecon-phase-5-registry-spec.md`](./beecon-phase-5-registry-spec.md)). It finally builds
> what Phase 4 **PD39 deferred**: real per-operator accounts, passwords, and sessions,
> replacing the single static installation-wide `BEECON_ADMIN_API_KEY` gate the Admin UI
> ships behind today.
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 5 milestone; access
> management). Boundaries: `.claude/BOUNDARIES.md` (`access/`). Context:
> `.claude/bee-context.local.md`. Design brief: `.claude/DESIGN.md` (§5 login/session
> surface — the slot is already reserved). Builds on shipped Phase 1–4:
> [phase-1](./beecon-phase-1-spec.md), [phase-2](./beecon-phase-2-spec.md),
> [phase-3](./beecon-phase-3-spec.md), [phase-4](./beecon-phase-4-spec.md).
> PD numbering continues from Phase 4 (last was PD48) — this sub-phase reclaims **PD49+**.

## Overview

Today the entire Admin UI and every admin API route sit behind **one shared static key**
(`BEECON_ADMIN_API_KEY`, constant-time Bearer compare in
`access/driving/authmw/admin.go`). There is no per-operator identity, no attribution, no
revocation short of rotating the installation-wide key, and the SPA holds that key in tab
memory (Phase 4 PD39, an explicit trusted-network trade-off). This sub-phase replaces that
gate with **real operator authentication**: per-operator accounts (email + memory-hard-KDF
password), a real login screen (the DESIGN.md §5 slot), server-set revocable cookie
sessions backed by the existing database, and CSRF protection — so the console can be
safely exposed beyond a locked-down private network and every mutating action can be
attributed to a person.

Risk: **HIGH** — this is authentication for the credential-handling operator console.
Failure modes are first-class: credential leakage (plaintext passwords or session tokens
anywhere), session fixation/replay, CSRF, account lock-out (including locking out the last
operator), and brute-force. Because it is HIGH risk, slices carry 8–10 ACs covering happy
path, error cases, and security invariants.

### Phase 5 milestone map (reframed — registry deferred)

Phase 5 ("Ecosystem") is delivered as sub-phases. With the registry deferred, the
non-registry roadmap is:

- **Operator auth (this spec)** — real per-operator accounts, sessions, CSRF. *(strand 7,
  Phase 4 PD39 deferral.)*
- **SDK polish + Membrane migration importer** *(next; roadmap)* — AI-framework tool
  adapters (OpenAI / Mastra tool shapes), multi-endpoint webhook SDK resources (deferred
  from Phase 4), docs/quickstart; plus the Membrane-export → Beecon definition importer
  (input = the `temp/*.yaml` samples) and a per-operation SDK migration guide. Precedes
  Rolai adoption because it is that work's prerequisite.
- **Service-bus delivery adapter** *(roadmap)* — a second delivery channel (Azure Service
  Bus for eCW) behind a delivery `Channel` port, chosen per organization.
- **Rolai adoption plan** *(roadmap)* — add Beecon as a third provider in rolai's
  `IntegrationRoutingService`, migrate provider-by-provider, retire Membrane + Composio.
  Proposed as an out-of-repo integration plan (rolai is a separate codebase).
- **Registry service + `tool_` ids** — **explicitly deferred to a later date** (developer
  decision, 2026-07-15). Preserved as a deferred artifact; PDs to be renumbered on revival.

Each roadmap item gets its own spec (own PD block, own ACs) when picked up. Only operator
auth is specced in detail here.

## Proposed Decisions

> **Developer-confirmed 2026-07-15** (via post-spec Q&A relayed through the orchestrator —
> AskUserQuestion being unavailable inside the subagent, as in the Phase 2/3/4 builds). All
> seven flagged decisions were answered:
> - **PD50 (KDF): CONFIRMED explicitly** — Argon2id, and the `golang.org/x/crypto/argon2`
>   dependency is accepted.
> - **PD53 (SSO/OIDC): CONFIRMED explicitly as DEFERRED** — local email+password only this
>   sub-phase; SSO/OIDC is a later sub-phase.
> - **PD54 (bootstrap): CONFIRMED explicitly** — first operator bootstrapped from
>   `BEECON_ADMIN_API_KEY`; the admin key is then **demoted to break-glass** (still works
>   for emergency recovery, not normal use).
> - **PD55 (login path): CONFIRMED explicitly** — email/password login is the **only**
>   console path once accounts exist; the in-memory admin-key gate is **removed from the
>   SPA** (the break-glass admin key is a documented recovery, not a UI path).
> - **PD51 (session store): CONFIRMED as proposed** — DB-backed `operator_sessions`
>   (bun Postgres/SQLite + memory, no Redis), opaque token with only its SHA-256 hash stored.
> - **PD52 (cookie/CSRF): CONFIRMED as proposed** — HttpOnly+Secure+SameSite=Strict session
>   cookie + double-submit CSRF via `X-CSRF-Token`.
> - **PD56 (attribution): CONFIRMED as proposed** — capture operator identity on mutating
>   admin actions now; full audit-log UI later.
> - **PD57 (authz): CONFIRMED as proposed** — flat (all operators equal this sub-phase;
>   roles/RBAC later).
>
> PD49, PD58 stand as reasoned proposals grounded in the discovery doc,
> `.claude/BOUNDARIES.md`, `.claude/DESIGN.md` §5, and the existing `access` module.
> Numbering continues from Phase 4 (PD39–PD48).

1. **PD49 — Operator auth replaces PD39's static-admin-key gate with real per-operator
   accounts, and is the lead Phase 5 sub-phase.** The console's users become identified
   operators with individual credentials and revocable sessions. This is the whole of this
   sub-phase; the registry and other strands are separate (milestone map above).
2. **PD50 — Password hashing uses Argon2id (memory-hard), via
   `golang.org/x/crypto/argon2`.** Discovery explicitly calls for a "memory-hard-KDF
   password"; Argon2id is the OWASP-recommended default and resists GPU/ASIC cracking
   better than bcrypt (memory-hard) and is the id variant that also resists side-channel
   attacks. Parameters follow current OWASP guidance and are **fixed in code, not config**
   (a wrong KDF cost is a security bug, not an operator knob). **Dependency flag:** the
   codebase has been deliberately dependency-frugal (crypto/rand, crypto/sha256, crypto/
   subtle from the stdlib for existing secrets); Argon2id adds `golang.org/x/crypto` —
   effectively quasi-standard-library (Go team maintained), low-risk, but it *is* a new
   direct dependency. Alternatives if the developer prefers zero new deps: `scrypt` (also
   in `golang.org/x/crypto`, same dependency cost) or bcrypt. **Confirm Argon2id + the
   dependency.**
3. **PD51 — Sessions are server-side, stored in the existing database via a new
   `operator_sessions` table, with a bun adapter (Postgres + SQLite) and a memory adapter**
   — matching every other repository in `access/` (`driven/bun` + `driven/memory`). No
   external session store (Redis) — that would break the "small, single-binary" principle
   (H5). The session cookie carries an **opaque random token** (`crypto/rand`, ~32 bytes,
   the existing secret-entropy convention); only a **SHA-256 hash** of the token is stored
   (mirroring `access/secret.go`'s `hashSecretRemainder` + constant-time compare) — a
   database leak never yields a usable session. Revocation = deleting/marking the row.
   **Confirm the DB-backed session store.**
4. **PD52 — The session cookie is `HttpOnly`, `Secure`, `SameSite=Strict`; CSRF is a
   double-submit token the SPA echoes in a header.** `SameSite=Strict` already blocks
   cross-site cookie sends; the double-submit CSRF token (a non-`HttpOnly` token cookie the
   SPA reads and re-sends as `X-CSRF-Token`, validated against the session server-side) is
   defense-in-depth on mutating requests, and its custom header cannot be forged cross-
   origin without a CORS preflight. Chosen over a synchronizer token (no server-side
   per-form token storage needed — the token is bound to the session). **Confirm the
   cookie flags + double-submit CSRF strategy.**
5. **PD53 — Local email+password accounts only this sub-phase; SSO/OIDC is a later
   sub-phase.** Getting local accounts, sessions, and CSRF right is the security-critical
   core; OIDC (Azure AD for eCW is the likely driver) is additive and can slot behind the
   same session mechanism later. The DESIGN.md §5 login card already leaves room for an SSO
   button. **Confirm OIDC is deferred (vs in-scope now).**
6. **PD54 — The first operator is bootstrapped from the existing `BEECON_ADMIN_API_KEY`;
   after operators exist, the admin key is retained as a break-glass credential only.** A
   `POST /api/v1/operators/bootstrap` endpoint, authenticated by the admin key, creates the
   initial operator account **only when no operator account exists yet** (and serves as a
   break-glass reset path thereafter). Once at least one operator exists, the static admin
   key **no longer authenticates general `/api/v1/*` calls** — it is demoted to the
   break-glass bootstrap/reset endpoints only. This keeps a recovery path (lost-password,
   no operators can log in) without leaving a full-access shared key live. **Confirm:
   admin key demoted-to-break-glass (vs fully retired, vs kept as a co-equal path).**
7. **PD55 — Once accounts exist, login is the only console path; the Phase 4 in-memory
   admin-key gate is removed from the SPA.** The SPA's Phase 4 gate screen (admin key held
   in tab memory) is replaced by the DESIGN.md §5 email/password login form that POSTs to
   `/api/v1/auth/login` and relies on the session cookie thereafter — the SPA holds no
   credential in JS memory. **Confirm login-only (vs keeping the admin-key gate as a
   visible fallback in the UI).**
8. **PD56 — Mutating admin actions capture the acting operator's id for attribution; a
   full audit-log UI is deferred.** Now that requests carry an operator identity (via the
   session), the authenticated operator id is available in context and stamped on mutating
   actions / their log lines (reusing the request-logging path). A browsable operator
   audit-log surface in the console is a later item (grouped with the deferred Phase 4
   purge rows-deleted audit line — see carry-forwards). **Confirm attribution-now,
   audit-UI-later.**
9. **PD57 — All operators are equal (full installation access); no roles/RBAC this
   sub-phase.** This matches today's blast radius exactly — the single admin key already
   grants full access, so flat operator accounts are not a regression, and RBAC is a
   sizeable design in its own right (YAGNI until a real least-privilege requirement
   arrives). **Confirm flat authz (vs building roles now).**
10. **PD58 — Operator accounts + sessions live in the `access/` module; auth middleware
    joins `access/driving/authmw`.** BOUNDARIES gives `access/` ownership of installation
    auth (ServerApiKey, UserToken, WebhookSigningSecret, verification). Operator accounts
    and sessions are installation-level auth of the same kind; they are **not** org-scoped
    (an operator administers the whole installation, like the admin key). The session/CSRF
    middleware sits beside `admin.go` in `authmw`. New id prefix **`op_`** for operator
    accounts joins the CUID2 set (a BOUNDARIES addition this sub-phase makes).

---

## Slice 1 — Walking skeleton: bootstrap the first operator and log in

The thinnest end-to-end path a person experiences: they open the console, see a real login
screen (not the admin-key gate), the first account is bootstrapped from the admin key, and
they log in with email + password to land in the shell on a cookie session.

- [x] The Admin UI presents a login screen (email + password) in place of the Phase 4 in-memory admin-key gate
- [x] Using the existing `BEECON_ADMIN_API_KEY` once, an operator can bootstrap the first operator account (email + password) when no operator account exists
- [x] Attempting to bootstrap when an operator account already exists is rejected (bootstrap is first-account-only, break-glass thereafter)
- [x] An operator can log in with a correct email + password and lands in the console shell
- [x] On successful login the server sets a session cookie that is `HttpOnly`, `Secure`, and `SameSite=Strict`, and the SPA holds no admin key in memory
- [x] A subsequent API request carrying the session cookie is authenticated as that operator
- [x] Login with a wrong password, or an unknown email, is rejected with a single generic "invalid credentials" message that does not reveal which was wrong
- [x] A failed login shows an inline error (icon + text, never color-only) without clearing the typed email
- [x] Passwords are stored only as an Argon2id hash — the plaintext never appears in storage, logs, or any API response
- [x] A password shorter than the minimum length is rejected at account creation/bootstrap with a validation error naming the requirement
- [x] The session cookie carries an opaque random token whose SHA-256 hash (not the token) is what is stored server-side

## Slice 2 — Logout, expiry, and revocation

A session is a revocable server-side fact, not just a cookie — ending it, expiring it, or
disabling its operator all invalidate it immediately, and it cannot be replayed.

- [x] An operator can sign out from the top bar; the session is deleted/revoked server-side and the cookie is cleared
- [x] A request bearing a revoked session cookie is rejected as unauthenticated (401) and the SPA returns to the login screen
- [x] A session older than its absolute expiry (`BEECON_SESSION_TTL`) is rejected as unauthenticated even if the cookie is still present
- [x] Changing an operator's password revokes all of that operator's other active sessions <!-- carry-forward closed in Slice 4: ChangeMyPassword + RevokeAllForOperatorExcept keep the acting session (id from context, never the body) while revoking every other session. Verified 2026-07-15. -->
- [x] Deactivating an operator account immediately invalidates that operator's existing sessions
- [x] A revoked or expired session cannot be resurrected by replaying its old cookie value
- [x] Logging out is idempotent — a second logout (or one with no session) returns cleanly, not a 500

## Slice 3 — CSRF protection on mutating requests

Cross-site forgery cannot ride the operator's cookie: every state-changing call must carry
a session-bound CSRF token, and safe reads do not.

- [x] A state-changing request (POST/PUT/PATCH/DELETE) from an authenticated session must carry a valid CSRF token; a missing or mismatched token is rejected with a CSRF error
- [x] Safe requests (GET/HEAD) do not require a CSRF token
- [x] The CSRF token is bound to the session, so a token issued for one session is rejected on another
- [x] The SPA obtains its CSRF token via the documented mechanism and echoes it as `X-CSRF-Token` on every mutating call automatically
- [x] The login and logout endpoints are themselves protected against cross-site forgery
- [x] A rejected-CSRF response never leaks the expected token value

## Slice 4 — Operator account management and attribution

Operators manage each other, protect against total lock-out, and every mutating action is
attributable — and the static admin key steps down to break-glass.

- [x] A logged-in operator can create another operator account (email + initial password)
- [x] Creating an operator with an email that already exists is rejected with a validation error
- [x] An operator can list operator accounts showing email, status (active/disabled), and created date — never a password hash
- [x] An operator can change their own password after presenting their current password; a wrong current password is rejected
- [x] An operator can deactivate another operator account, after which that account can no longer log in
- [x] Deactivating the last remaining active operator is rejected with a clear error (prevents total lock-out)
- [x] Mutating admin actions record the acting operator's id for attribution (visible in the action's log line)
- [x] Once at least one operator account exists, the static admin key authenticates only the break-glass bootstrap/reset endpoints — a general `/api/v1/*` call presenting only the admin key is rejected

## Slice 5 — Login hardening and the re-authenticate experience

Brute force is throttled, lock-out does not leak account existence, and a session expiring
mid-work is a graceful re-auth, not lost work — with the login surfaces verified for
accessibility (Phase 4 carry-forward).

- [x] After a configurable number of consecutive failed logins for an account, further attempts are locked for a cooldown window
- [x] A successful login resets the failed-attempt counter for that account
- [x] The lockout is per-account and its response does not reveal whether the email exists
- [x] A session expiring mid-use surfaces a re-authenticate modal over the current page (preserving in-progress work), per DESIGN.md §5, rather than a hard bounce to login
- [x] Re-authenticating in the modal restores the session and the operator resumes where they were
- [x] The login screen and re-auth modal meet the design-brief accessibility bar: visible `:focus-visible` ring, 44×44px targets, WCAG AA contrast, `prefers-reduced-motion` respected, and are fully keyboard-operable (Phase 4 carry-forward: browser/a11y verification of the Admin UI login surface) <!-- Verified 2026-07-15: automatable a11y (focus-trap start, keyboard-operable, non-dismissible Esc/overlay, icon+text error) tested; 44px min-h-11 targets, :focus-visible rings, AA-verified design tokens, and motion-safe transitions present in code. Runtime WCAG contrast measurement deferred per FD-I (no browser MCP) — documented accepted trade-off. -->

---

## API Shape (indicative)

```
=== Bootstrap / break-glass (admin key; PD54) ===
POST /api/v1/operators/bootstrap        Authorization: Bearer <BEECON_ADMIN_API_KEY>
     { email, password }  -> 201 { id: "op_<cuid2>", email }
     (409 if an operator already exists — first-account-only; also the break-glass reset path)

=== Auth (cookie session; PD51/PD52) ===
POST /api/v1/auth/login    { email, password }
     -> 204  Set-Cookie: beecon_session=<opaque>; HttpOnly; Secure; SameSite=Strict; Path=/
             Set-Cookie: beecon_csrf=<token>; Secure; SameSite=Strict; Path=/   (readable by SPA)
     -> 401 { error: "invalid credentials" }   (same body for wrong password OR unknown email)
     -> 429 when the account is in a failed-attempt lockout
POST /api/v1/auth/logout   (session cookie + X-CSRF-Token)  -> 204 + cleared cookies (idempotent)
GET  /api/v1/auth/me       (session cookie)                 -> { id, email }   (SPA session probe)

=== Operators (session cookie + X-CSRF-Token on mutations; PD56/PD57 flat authz) ===
GET  /api/v1/operators                        -> { items: [{ id, email, status, createdAt }] }
POST /api/v1/operators                        { email, password } -> 201 { id, email }
POST /api/v1/operators/me/password            { currentPassword, newPassword } -> 204 (revokes other sessions)
POST /api/v1/operators/{opId}/deactivate      -> 204   (409 if it is the last active operator)

Data (access/ module; PD58):
  OperatorAccount: { id: "op_<cuid2>", email (unique), passwordHash (argon2id),
                     status: ACTIVE|DISABLED, failedAttempts, lockedUntil?, createdAt }
  OperatorSession: { id, operatorId, tokenHash (sha256), csrfToken, createdAt, expiresAt, revokedAt? }
```

## Out of Scope (operator-auth sub-phase)

- **SSO / OIDC** (PD53) — later sub-phase; the login card leaves the slot.
- **Roles / RBAC / per-operator permissions** (PD57) — all operators are equal this phase.
- **Email-based password reset / email verification** — Beecon has no email infrastructure;
  password recovery is via another operator (change/reset) or the break-glass admin-key
  bootstrap. No SMTP is added.
- **Multi-factor authentication (TOTP/WebAuthn)** — later, behind the same session mechanism.
- **A browsable operator audit-log UI** (PD56) — attribution is captured now; the viewer is
  a later item (grouped with the deferred Phase 4 purge audit line).
- **Changes to org-facing auth** — org **server API keys**, **user tokens**, and
  **webhook signing secrets** are unchanged; this sub-phase is operator/console auth only.
- **Registry + `tool_` ids, SDK polish, Membrane importer, service bus, Rolai adoption** —
  their own sub-phases (milestone map).

## Technical Context

- **Boundaries (binding):** operator accounts and sessions live in the **`access/` module**
  (BOUNDARIES: `access/` owns installation auth — keys, tokens, verification). They are
  **installation-level, not org-scoped** (an operator administers the whole installation,
  like the admin key), so the org-scope arch test (`orgscope_test.go`) does not apply to
  them — mirror how `admin.go`/`AdminAuth` is installation-wide today. The auth middleware
  (session validation, CSRF check) joins `access/driving/authmw` beside `admin.go`.
- **Persistence (binding):** new `operator_accounts` and `operator_sessions` repositories
  follow the module's existing **`driven/bun` (Postgres + SQLite) + `driven/memory`**
  pattern (every `access` repo already has both). Session tokens are hashed with
  **SHA-256 + constant-time compare**, reusing the exact convention in
  `access/secret.go` (`hashSecretRemainder`/`secretMatchesHash`); token entropy via
  `crypto/rand` (~32 bytes, the `secretEntropyBytes` convention).
- **Password KDF (binding, PD50):** Argon2id via `golang.org/x/crypto/argon2`, OWASP
  parameters fixed in code. This is the one new direct dependency (flagged); it is Go-team
  maintained and quasi-stdlib.
- **Sessions/CSRF fit the single binary (binding):** the session store is the existing
  database (no Redis, no external store — "keep it small", H5); cookies are set and read by
  the same `beecon serve` binary that serves `/admin` and `/api/v1`. `SameSite=Strict` +
  `Secure` + `HttpOnly` on the session cookie; a readable double-submit CSRF token cookie +
  `X-CSRF-Token` header on mutations (PD52).
- **Admin-key transition (binding, PD54/PD55):** `AdminAuth` stays for the break-glass
  bootstrap/reset endpoints; a new session-auth middleware guards the general `/api/v1/*`
  console surface once operators exist. The Phase 4 `GET /admin/verify` gate and the SPA's
  in-memory-key gate are replaced by the login flow.
- **Ids (binding):** new prefix **`op_`** (CUID2, `github.com/akshayvadher/cuid2`) for
  operator accounts — a BOUNDARIES addition this sub-phase makes (sessions use an internal
  id + the opaque token; no user-facing prefix needed for the token).
- **Conventions (binding):** `httpx` DomainError envelope for every error; secrets/tokens
  never logged or serialized; constant-time compares for password and token verification;
  request-logging middleware continues to exclude secret paths (add `/api/v1/auth/*` bodies
  to the exclusion so passwords never reach logs).
- **New config:** `BEECON_SESSION_TTL` (default e.g. 12h), `BEECON_LOGIN_MAX_ATTEMPTS`
  (default 5), `BEECON_LOGIN_LOCKOUT` (default 15m). All `BEECON_*` typed and fail-fast per
  `config/config.go`. Argon2id cost parameters are code constants, not config (PD50).
- **Design (binding):** the login screen and re-auth modal follow `.claude/DESIGN.md` §5
  (centered ~400px card, wordmark, inline error banner never color-only, re-auth modal over
  the page, same focus-ring/44px/radii tokens as the rest of the console).
- **Phase 4 carry-forwards:** the login surface's **browser/a11y verification** is folded
  into Slice 5. The **purge worker's rows-deleted audit-log line** and the **`RequireWrite`
  arch test on every org-key mutating route** are small, unrelated backend hygiene items —
  **roadmap/optional**, to be done as tidy alongside this sub-phase if trivial, otherwise
  carried to the next backend sub-phase; they are **not** operator-auth slice ACs.
- Risk level: **HIGH**

---

- [x] Reviewed <!-- approved by developer 2026-07-15 (via post-spec Q&A); PD50/PD53/PD54/PD55 confirmed explicitly, PD51/PD52/PD56/PD57 accepted as proposed -->
