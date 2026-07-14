# Beecon Admin UI — Design Brief

> **Type:** Greenfield UI design brief (first run).
> **Scope:** Beecon Phase 4 Admin UI — an operator-facing web console for a self-hosted
> integration platform. Covers organizations, users, API keys, connections, trigger
> instances, a log explorer, provider definitions, events / webhook delivery, metrics /
> operability dashboards, org-level governance, and a login / session surface.
> **Triage:** EPIC, HIGH risk (operator console over credential-handling infrastructure).
> **Continuity anchor:** the ad-hoc palette already shipped in the Go-template middle-man
> connect pages (`server/internal/connectweb/templates/*.gohtml`) is carried forward so the
> admin console and the connect pages read as one product.

> **Not a design decision — decided in spec/architecture, recorded here for context only:**
> the frontend stack (**TanStack Start** SPA in `apps/admin-ui/`) and the **operator
> authentication mechanism** (today the backend has only a single static installation-wide
> `BEECON_ADMIN_API_KEY` Bearer key — real operator login / sessions / CSRF are a Phase 4
> spec/architecture item). This brief describes **UX, visuals, and the component contract**,
> not framework choices or the auth protocol. Where the login screen is described, it is the
> screen's layout/states — not the mechanism behind it.

---

## 0. Design Decisions — developer-confirmed 2026-07-14

The six open visual/UX decisions were relayed to the developer and **all confirmed** on
2026-07-14. The values below are settled; downstream steps (spec, architecture, programming)
inherit them. Alternatives are retained only as a record of what was considered.

| # | Decision | Confirmed value (developer-confirmed 2026-07-14) | Alternatives considered |
|---|----------|--------------------------------------------------|-------------------------|
| 1 | Aesthetic / density | **Confirmed: Linear-style minimal shell + Temporal-grade density on data surfaces** (hybrid) | Pure Linear-minimal; pure Temporal-dense; PocketBase-utilitarian |
| 2 | Theme(s) | **Confirmed: both light + dark**, token system built for both from day one | Light only (dark-ready); dark only |
| 3 | Palette | **Confirmed: carry `#2563eb` forward** as primary; neutrals = the same gray ramp the connect pages use | Fresh operator palette; blue-as-accent-only |
| 4 | Detail-drill pattern | **Confirmed: right-side drawer** for scan-heavy surfaces (logs, events, connections); **full page** for config-heavy surfaces (trigger instances, provider definitions) | Drawer everywhere; full-page everywhere |
| 5 | Navigation shape | **Confirmed: persistent grouped left sidebar** (collapsible to icon rail) + slim top bar | Top-nav |
| 6 | Component approach | **Confirmed: headless lib + Tailwind** — accessible behavior comes from a headless primitive library, styled with the tokens in this brief. The **exact library is an architecture decision** that must still meet the §7 component contract. | Hand-built primitives on Tailwind |

---

## 1. Design Direction & Principles

Beecon's Admin UI is an **operator console**, not a marketing surface or a consumer app.
Operators are technical, they keep it open for long sessions, and they use it to triage —
"which connections are expiring, which webhook deliveries failed, what's in this redacted
log body". The visual language serves scanning, trust, and precision.

**Personality (1-2 sentences):** Calm, engineered, and dense-when-it-needs-to-be. A quiet
minimal shell (Linear) wrapping high-information data surfaces (Temporal), with the
straightforward pragmatism of a self-hosted admin (PocketBase) — nothing decorative competes
with the data.

**Principles**

1. **Data first, chrome last.** The shell recedes; tables, logs, and status are the
   subject. Borders and spacing do the structuring, not color fills or shadows.
2. **Density is a mode, not a default trap.** Comfortable by default; a compact toggle on
   data tables for operators triaging thousands of rows. Machine values (ids, timestamps,
   payloads) render in monospace.
3. **Status is unmissable and never color-only.** Every state pairs color + icon + text
   label (see §7 badges, §9 accessibility). An operator scanning a failed-delivery list must
   see it in grayscale.
4. **Secrets are handled with ceremony.** API keys, webhook signing secrets, and user tokens
   are shown exactly once. The key-shown-once modal (§7) is a first-class, deliberately
   high-friction component — this is a credential-handling console.
5. **Progressive disclosure over mega-forms.** Governance rules, key scopes, provider
   definitions, and trigger config are deep; use tabs, drawers, collapsible sections, and
   steppers. Destructive and primary actions are never hidden.
6. **Continuity with the connect pages.** Same primary blue, same gray ramp, same radii,
   same focus-ring — an operator moving between the admin console and a connect page should
   feel one product.
7. **Accessibility is priority 1.** WCAG AA contrast, visible focus, 44px targets, keyboard
   operability, and `prefers-reduced-motion` are constraints, not polish.

---

## 2. Color Palette

All colors are exposed as CSS custom properties / Tailwind theme tokens — **never hardcode
hex in components**. Neutrals use the Tailwind **gray** ramp because that is exactly what the
connect pages already use (`#e5e7eb`, `#d1d5db`, `#6b7280`, `#4b5563`, `#111827`), preserving
continuity. Contrast ratios below are against the relevant surface and all meet WCAG AA.

### 2.1 Brand / primary (carried forward from the connect pages)

| Token | Value | Notes |
|-------|-------|-------|
| `--primary` | `#2563eb` | blue-600. White text on this = **5.17:1** (AA). As link text on white = 5.17:1, on canvas ≈ 4.83:1 (AA). |
| `--primary-hover` | `#1d4ed8` | blue-700 (carried forward). |
| `--primary-active` | `#1e40af` | blue-800. |
| `--primary-fg` | `#ffffff` | text/icon on primary fill. |
| `--focus-ring` | `#93c5fd` (light) / `#60a5fa` (dark) | carried forward from connect pages. |

### 2.2 Light theme

| Role | Token | Value | Contrast |
|------|-------|-------|----------|
| App canvas | `--bg` | `#f5f6f8` | carried forward |
| Card / surface | `--surface` | `#ffffff` | — |
| Muted surface | `--surface-muted` | `#f3f4f6` (gray-100) | — |
| Border | `--border` | `#e5e7eb` (gray-200) | carried forward |
| Border strong / inputs | `--border-strong` | `#d1d5db` (gray-300) | carried forward |
| Text primary | `--text` | `#111827` (gray-900) | 16.1:1 on white (AAA) |
| Text secondary | `--text-secondary` | `#4b5563` (gray-600) | 7.6:1 on white (AAA) |
| Text muted | `--text-muted` | `#6b7280` (gray-500) | 4.8:1 on white (AA) — use on white/cards, not on tinted fills |

### 2.3 Dark theme

| Role | Token | Value | Contrast |
|------|-------|-------|----------|
| App canvas | `--bg` | `#0b0f17` | near-black, faint blue cast |
| Card / surface | `--surface` | `#111827` (gray-900) | — |
| Muted surface | `--surface-muted` | `#1f2937` (gray-800) | — |
| Border | `--border` | `#1f2937` (gray-800) | — |
| Border strong / inputs | `--border-strong` | `#374151` (gray-700) | — |
| Text primary | `--text` | `#f3f4f6` (gray-100) | ~15:1 on surface |
| Text secondary | `--text-secondary` | `#9ca3af` (gray-400) | ~7:1 on surface |
| Text muted | `--text-muted` | `#6b7280` (gray-500) | ~4.6:1 on surface (AA) |
| Accent text / links | `--accent-text` | `#60a5fa` (blue-400) | 6.97:1 on `#111827` — use instead of `#2563eb` for text on dark |

Primary **fills** stay `#2563eb` with white text in both themes (5.17:1). Blue as **text** on
dark switches to `#60a5fa` for contrast.

### 2.4 Semantic colors (both themes)

Each semantic role has three tokens: `-solid` (fills, icons, dots), `-text` (text on the
light/tinted background — uses the darker shade to pass AA), `-bg` (tint background for
badges/banners). Semantic color is **always** accompanied by an icon and a text label.

| Role | `-solid` | `-text` (light) | `-bg` (light) | Dark note |
|------|----------|-----------------|---------------|-----------|
| Success | `#16a34a` (green-600) | `#15803d` (green-700, 4.9:1) | `#dcfce7` | text `#4ade80`, bg `rgba(22,163,74,.15)` |
| Warning | `#d97706` (amber-600) | `#b45309` (amber-700, ~5:1) | `#fef3c7` | text `#fbbf24`, bg `rgba(217,119,6,.15)` |
| Error | `#dc2626` (red-600) | `#b91c1c` (red-700, 6.5:1 — carried from connect pages) | `#fee2e2` | text `#f87171`, bg `rgba(220,38,38,.15)` |
| Info | `#2563eb` (primary) | `#1d4ed8` (blue-700, 6.9:1) | `#dbeafe` | text `#60a5fa`, bg `rgba(37,99,235,.15)` |
| Neutral | `#6b7280` (gray-500) | `#4b5563` | `#f3f4f6` | text `#9ca3af`, bg `rgba(148,163,184,.15)` |

**60-30-10:** ~60% canvas + surfaces (grays), ~30% supporting surfaces/borders/secondary
text, ~10% primary blue + semantic accents reserved for CTAs, active nav, and status. Data
should be able to shout in color precisely because the shell is quiet.

---

## 3. Typography

**Pairing (two families, one superfamily — cohesive and intentional, avoids generic
defaults):**

- **UI / headings / body:** **IBM Plex Sans** — a humanist sans with genuine engineering
  character; headings are the same family at heavier weights (no separate display font).
- **Machine values:** **IBM Plex Mono** — ids (`conn_…`, `tool_…`, `trg_…`), timestamps,
  redacted log payloads, JSON/YAML provider definitions, code.

> Alternative pairing the developer may swap in without other changes: **Geist Sans + Geist
> Mono**. Do **not** fall back to Inter/Roboto/system-default as the primary — the connect
> pages use a system stack only because they are minimal server-rendered pages; the admin
> console gets an intentional face.

**Type scale** (data-dense console — base UI is 14px; 12–13px used in dense tables):

| px / rem | Use |
|----------|-----|
| 12 / 0.75 | micro: table meta, badge text, timestamps |
| 13 / 0.8125 | dense table cells, secondary labels, inline mono ids |
| 14 / 0.875 | **base** UI / body |
| 16 / 1 | emphasized body, form inputs |
| 18 / 1.125 | section subheading |
| 20 / 1.25 | card / section title |
| 24 / 1.5 | page title (H1) |
| 30 / 1.875 | top-level dashboard headline (rare) |

**Weights:** 400 body · 500 medium (labels, table headers, nav) · 600 semibold (headings,
buttons) · 700 bold (page titles, sparingly).
**Line-height:** 1.5 body · 1.4 dense table rows · 1.25 headings · 1.5 mono payload blocks.
**Measure:** cap prose/help-text at ~70ch; tables and payloads are exempt.

---

## 4. Spacing & Layout System

**Spacing scale — 4px base:** `4 · 8 · 12 · 16 · 20 · 24 · 32 · 40 · 48 · 64`. No off-scale
values. Grouped elements get tighter spacing (proximity); sections get more space above than
below.

**Radii** (carried forward + extended): `--radius-sm 6px` (badges, chips) · `--radius-md 8px`
(inputs, buttons — carried forward) · `--radius-lg 12px` (cards, panels, modals — carried
forward) · `--radius-pill 9999px` (status pills).

**Elevation (minimal — Linear-quiet):** rely on borders first; shadows only where a surface
truly floats.
- `--shadow-sm` `0 1px 2px rgba(0,0,0,.06)` — hover/raised rows.
- `--shadow-md` `0 4px 12px rgba(0,0,0,.08)` — dropdowns, popovers.
- `--shadow-lg` `0 12px 32px rgba(0,0,0,.16)` — drawer, modal (with scrim).
In dark theme, prefer a lighter border (`--border-strong`) over shadow to signal elevation.

**Grid & containers:**
- Data tables: **full-width** within the content area (they need the room).
- Forms, detail, settings: constrained to a **max-width ~1040px** reading column.
- Dashboard: 12-column responsive grid of metric tiles (`gap: 16px`).
- Detail drawer: **480–560px**, resizable, right-anchored, with scrim on mobile only.

**Responsive:** desktop-first for a desktop operator tool, but must not break down. Breakpoints
375 / 768 / 1024 / 1440. Below ~1024px: sidebar collapses to an icon rail / off-canvas;
drawers become full-screen sheets; tables gain horizontal scroll within their container (no
page-level horizontal scroll ever).

---

## 5. Navigation Structure (the ~9 areas)

**Shape:** persistent **left sidebar** (~248px, collapsible to a ~56px icon rail) + a slim
**top bar**. This suits ~9 areas better than top-nav and matches all three references.

**Top bar** (left→right): product wordmark · **organization switcher** (an installation hosts
many orgs; this scopes org-level views) · spacer · **command palette** trigger (`Cmd/Ctrl-K`,
Linear-style) · theme toggle (light/dark) · operator/session menu (sign out).

**Sidebar — grouped so 9+ areas stay scannable:**

- **OBSERVE**
  - Dashboard (metrics / operability tiles)
  - Logs (log explorer, redacted bodies)
  - Events & Delivery (events list, webhook deliveries, redeliver)
  - Metrics (Prometheus `/metrics`-backed, admin-guarded)
- **OPERATE**
  - Connections (org-scoped, cursor-paginated)
  - Trigger Instances
- **CATALOG**
  - Providers (provider definitions, versioned bundles)
  - Tools
  - Trigger Definitions
- **ADMINISTER**
  - Organizations (installation-wide — list-all is Phase-4-new)
  - Users (per org)
  - API Keys (create / rotate / scope)
- **GOVERN**
  - Governance (integration allow-lists, visibility rules, onboarding defaults — org-scoped)
  - Settings (retention config + purge, webhook endpoints + per-endpoint event-type filters)

**Installation-scope vs org-scope:** views like *Organizations*, *Providers*, and *Metrics*
are **installation-wide** and sit above the org switcher; *Connections*, *Users*, *Trigger
Instances*, *Governance* are **org-scoped** and follow the selected org. Make the current
scope visible (e.g. the switcher shows "All organizations" vs a specific org, and org-scoped
pages show the org name in the breadcrumb). This mirrors the backend's org-scoping enforced at
the persistence port.

**Login / session surface (screen UX only — mechanism is a spec decision):**
- Centered card (max-width ~400px) on the app canvas, product wordmark above.
- Fields per whatever the auth spec lands on (e.g. email + password, and/or an SSO button as
  a placeholder slot); inline field validation; a single primary `#2563eb` CTA.
- Auth failure = inline error banner (error tokens, icon + text), never a full-page redirect
  that loses input.
- Session expiry mid-session = a **re-authenticate modal** over the current page (preserve
  in-progress work) rather than a hard bounce to login.
- Same focus-ring, 44px targets, and radii as the rest of the system.

---

## 6. Data-Surface Patterns (logs, events, connections)

- **Drill pattern:** right-side **drawer** for scan-heavy surfaces (Logs, Events & Delivery,
  Connections) so the list stays in view for fast row-to-row triage; **full page** for
  config-heavy surfaces (Trigger Instances, Provider Definitions).
- **Filter/search prominence:** a filter bar pinned above every data table — a prominent
  search input top-left, plus faceted filters (status, org, provider, date range) and
  applied-filter chips that are individually removable. This matches the backend's
  filterable, cursor-paginated Query surfaces.
- **Density:** comfortable default with a **compact toggle**; sticky header; borders over
  zebra striping; row hover raise.
- **Machine values:** ids/timestamps/payloads in IBM Plex Mono; long CUID2 ids truncated with
  a click-to-copy affordance; timestamps show relative text with the absolute value on hover.
- **Redaction:** redacted log/event bodies render with an explicit textual `[redacted]`
  marker (never color-only), so the redaction is unmistakable in the mono payload viewer.
- **Pagination:** opaque cursor-based controls (matches backend base64 cursors, default 50 /
  max 200) — "Load more" or prev/next, not numbered pages.

---

## 7. Core Component Inventory

The Admin UI needs, at minimum:

**Shell**
- Sidebar nav (grouped, collapsible icon rail), top bar (org switcher, command palette, theme
  toggle, session menu), breadcrumb.
- Command palette (`Cmd/Ctrl-K`): jump to org / entity / action.

**Data**
- **Data table:** sortable + sticky header, comfortable/compact density toggle, row hover,
  optional row selection, column visibility, cursor pagination, and first-class
  empty / loading (skeleton rows) / error states.
- **Filter bar:** prominent search, faceted filters, removable applied-filter chips,
  optional saved views.
- **Detail drawer (right panel):** header (title + status badge + actions), tabbed body,
  key/value definition lists, redacted-body / payload viewer, copy-id chips.
- **Detail full page:** config-heavy header + tabs + sections (trigger instance, provider
  definition).
- **Code / JSON / YAML viewer:** provider definitions, tool input/output JSON-Schema,
  redacted payloads — mono, collapsible, copy, subtle syntax tint (tint must not be the sole
  signal for anything).
- **Copy-to-clipboard id chip:** mono, for prefixed CUID2 ids (`conn_`, `tool_`, `trg_`).

**Forms & actions**
- **Forms:** labeled inputs, help text, inline `aria-describedby` validation, section
  grouping, progressive disclosure for advanced fields (key scopes, trigger config, governance
  rules), and a sticky action bar (primary/secondary) for long forms.
- **Key-shown-once modal (critical, credential-handling):** on create/rotate of an API key,
  webhook signing secret, or user token, the secret is displayed **exactly once** — mono
  field, copy button, download option, an explicit "You will not be able to see this again"
  warning, and a "I've stored it safely" checkbox that gates dismissal. Scope selection (once
  the backend gains key scopes) is presented in the same create flow.
- **Confirmation dialog:** for destructive actions (revoke key, delete connection, delete
  trigger instance); **type-to-confirm** for the highest-risk (revoke key, delete
  organization).
- **Toasts:** non-blocking success/error/info with icon + label.

**Status & feedback**
- **Status badges** — pill = `-bg` tint + `-text` + leading icon/dot + text label (color is
  never alone). Taxonomies:
  - *Connection:* `ACTIVE` (success · check) · `INITIATED` (neutral/info · clock) ·
    `DISCONNECTED` (error · x-circle) · `EXPIRED` (warning · alert-triangle).
  - *Trigger instance:* `ENABLED` (success · check) · `DISABLED` (neutral · pause) ·
    `ERROR` (error · alert-triangle).
  - *Webhook / event delivery:* `DELIVERED` (success · check) · `PENDING` (neutral · clock) ·
    `RETRYING` (warning · rotate) · `FAILED`/`DEAD` (error · x-circle).
  - *API key:* `ACTIVE` (success) · `ROTATING`/grace (warning · rotate) · `REVOKED` (error) ·
    `EXPIRED` (neutral/warning).
- **Empty states:** headline + one-line description + primary action (e.g. "No connections
  yet — connect a provider"). Light on illustration.
- **Loading:** skeleton rows for tables/lists; inline spinners for button-level actions only.
- **Error states:** inline error card with a Retry action; page-level error boundary as a
  fallback.

**Dashboard**
- **Metric tiles:** number + short label + trend/delta + optional sparkline.
- **Time-series charts:** operability metrics — series distinguished by more than color
  (labels, direct annotation, or pattern), tooltips, reduced-motion-aware.

---

## 8. Motion & Interaction

- Transitions **150–250ms** for UI feedback; ease-out for enters.
- Drawer slides in from the right; modal fades + subtle scale over a scrim.
- Skeleton shimmer and all non-essential motion **respect `prefers-reduced-motion`** (reduce
  to instant/opacity-only).
- Every interactive element has a hover state and a visible focus state; clickable non-links
  get `cursor: pointer`.

---

## 9. Accessibility Rules (priority 1 — blockers, not polish)

- **Contrast:** all text/background token pairs in §2 meet WCAG AA (4.5:1 normal, 3:1 large);
  verified inline. Text-muted is restricted to white/card surfaces where it clears AA.
- **Color is never the only signal:** status badges, delivery states, form validation, and
  charts all pair color with an icon/label/shape. Must remain legible in grayscale and under
  deuteranopia/protanopia.
- **Focus visibility:** every interactive element shows a `:focus-visible` ring using
  `--focus-ring` (carried forward from the connect pages). Never remove an outline without a
  replacement.
- **Touch/click targets:** 44×44px minimum (carried forward from the connect pages).
- **Keyboard:** all actions reachable and operable by keyboard; command palette is a
  first-class keyboard entry point; drawers and modals trap focus, close on `Esc`, and return
  focus to the trigger on close.
- **Semantic landmarks:** real `nav` / `main` / `aside` / `header`; tables use proper
  `th`/`scope`; form fields link errors via `aria-describedby`.
- **Redaction is textual:** redacted content carries a text marker, never color-only, so it
  is unmistakable to screen readers and in grayscale.
- **Motion:** honor `prefers-reduced-motion` everywhere.
- **Tokens, not hex:** components consume CSS variables / Tailwind theme tokens exclusively.
  Behavior/a11y primitives come from the confirmed headless lib (§0 #6); this brief's tokens
  supply the styling.

---

## 10. Continuity Notes (middle-man connect pages)

The Go-template connect pages (`connect` / `params` / `error` `.gohtml`) share this system's
primary (`#2563eb` / hover `#1d4ed8`), focus ring (`#93c5fd`), gray neutrals (`#e5e7eb`,
`#d1d5db`, `#6b7280`, `#4b5563`, `#111827`), error (`#b91c1c`), radii (8px/12px), 44px
targets, and `:focus-visible` outlines. The context notes those three templates duplicate
their inline CSS verbatim (a DRY tidy opportunity); if Phase 4 hardens the connect UI, extract
a shared stylesheet whose values are these tokens so the two surfaces stay locked together.

---

[ ] Reviewed
