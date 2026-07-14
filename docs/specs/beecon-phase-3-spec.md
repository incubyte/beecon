# Spec: Beecon Phase 3 — Events Out: Triggers, Signed Webhooks, Token Self-Healing

> Beecon starts talking back: first-class triggers, an outbox that cannot lose events,
> HMAC-signed webhook deliveries, and tokens that heal themselves in the background.
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 3 of the Milestone Map).
> Boundaries: `.claude/BOUNDARIES.md`. Context: `.claude/bee-context.local.md`.
> Builds on the shipped Phase 1 + 2: [beecon-phase-1-spec.md](./beecon-phase-1-spec.md),
> [beecon-phase-2-spec.md](./beecon-phase-2-spec.md).

## Overview

Everything so far has been request-path: the consumer calls, Beecon answers. Phase 3
adds the reverse direction. Trigger definitions become part of the provider-definition
format (replacing Membrane's `*-trigger` flow hack — see
`temp/outlook-message-received-trigger.yaml`); consumers create trigger instances bound
to Connections and receive fired events as **signed webhooks**. All outbound
notifications flow through one persisted **outbox**, delivered with HMAC signatures,
timestamps, idempotency ids, and retries — verified in one line by the SDK (validates
the H4-security signing design and closes rolai's "unauthenticated webhooks" TODO).
Tokens stop dying: a proactive refresh scheduler renews access tokens before expiry,
permanent refresh failure flips the connection to EXPIRED and emits a signed
`connection.expired` reverse message, and a reconciliation job catches connections the
provider killed behind our back.

Risk: HIGH — signed webhook delivery is a brand-new security surface (signature scheme,
replay protection, secret rotation), and the outbox/schedulers are the first background
processing in a codebase that has been strictly request-path. Failure modes (crash
mid-delivery, double-fire, refresh races) are first-class below.

## Proposed Decisions (synthesis — correct at review if wrong)

AskUserQuestion was unavailable during speccing; these are reasoned proposals, each
grounded in the discovery doc, the rolai context, the Membrane trigger sample, and the
Standard Webhooks specification. Numbering continues from Phase 2 (PD13–PD26).
**PD27, PD28, PD30, and PD31 are the four that most change the spec — review those
first.**

> **Developer-confirmed 2026-07-14** (via post-spec Q&A): PD27 (Standard Webhooks
> signature scheme), PD28 (poll-only ingestion, 60s default), PD30 (10-attempts/
> ~3-days retry schedule with manual redeliver), PD31 (one webhook endpoint per
> organization). The remaining PDs stand as proposals — correct at review if wrong.

1. **PD27 — Signature scheme: Standard Webhooks.** Deliveries carry the three
   spec-defined headers — `webhook-id` (the `evt_` id, stable across retries),
   `webhook-timestamp` (unix seconds), `webhook-signature` (space-delimited list of
   `v1,<base64>` values). Signed content is `{id}.{timestamp}.{raw body}`,
   HMAC-SHA256 with the organization's endpoint secret (`whsec_` + base64, 32 random
   bytes). Verifiers reject timestamps outside a ±5-minute tolerance. Choosing the
   open spec over a Stripe-style custom header buys: the idempotency id is bound into
   the signature, off-the-shelf verifier libraries work against Beecon out of the box,
   and the `whsec_` prefix matches Beecon's existing prefix conventions. The secret is
   stored encrypted with the existing vault key (HMAC needs the raw value —
   same reasoning as PD20).
2. **PD28 — Poll-only trigger ingestion in Phase 3.** The definition format's new
   `triggers` section declares `ingestion: poll` with a poll interval (default 60s —
   matching the Membrane sample — clamped to a platform minimum of 30s). Push
   ingestion (MS Graph change notifications, Hubspot app webhooks) is a later phase:
   it drags in inbound endpoint validation, subscription renewal jobs, and per-provider
   quirks that would dominate this phase. A definition declaring `ingestion: push`
   fails boot with a clear "not supported yet" message rather than being silently
   inert. The format field exists now so push arrives later without a format bump.
3. **PD29 — Background processing: in-process workers, no new runtimes.** The outbox
   dispatcher, trigger poller, refresh scheduler, and reconciliation job are goroutines
   inside the one `beecon serve` binary — no external queue, no cron, no second
   deployable ("keep it small" stands). Work items are claimed with short database
   leases (`FOR UPDATE SKIP LOCKED` on Postgres; SQLite's single-writer semantics
   suffice locally) so running two instances of the binary never double-delivers or
   double-refreshes. Graceful shutdown finishes or releases claimed work.
4. **PD30 — Delivery retry policy: the Standard Webhooks schedule.** A delivery
   attempt succeeds only on HTTP 2xx within a 10s timeout. Failures retry on the
   spec's recommended schedule — immediately, then ~5s, 5m, 30m, 2h, 5h, 10h, 14h,
   20h, 24h after the previous attempt (jittered) — 10 attempts spanning roughly three
   days. After exhaustion the event's delivery is marked `FAILED` and retained;
   `POST /api/v1/events/{id}/redeliver` re-queues it manually. Ordering is
   best-effort: events dispatch in creation order per organization, but a failing
   event never blocks newer ones (no head-of-line blocking) — consumers key on
   `webhook-id`, not arrival order. Endpoint auto-disable after persistent failure is
   deliberately not built (Phase 4 operability call).
5. **PD31 — One webhook endpoint per organization.** `PUT /api/v1/webhook-endpoint`
   (org key) sets the URL; the signing secret is generated server-side and returned
   exactly once at creation (URL changes keep the secret). Rolai has exactly one
   receiver today; multiple endpoints with event-type filters are Phase 4+ governance.
   The URL must be absolute http(s); org-key holders are trusted consumers of a
   self-hosted installation, so no private-address blocking — documented as an
   operator note. Secret rotation follows the PD23 pattern: new secret returned once,
   deliveries carry signatures from **both** secrets during the overlap window
   (default 24h, settable per rotation), old signature dropped after.
6. **PD32 — Event envelope and types.** Webhook body:
   `{id: "evt_...", type, createdAt, data}`. Phase 3 types: `trigger.event`
   (data: `{triggerInstanceId, triggerSlug, connectionId, userId, payload}` where
   `payload` conforms to the definition's payload schema), `connection.expired`
   (data: `{connectionId, userId, integrationId, providerSlug, reason}`), and
   `webhook.test` (data: `{}` — sent on demand to prove an endpoint works before
   anything real depends on it). The `evt_` id is the idempotency key: identical
   across retries and manual redeliveries; consumers deduplicate on it.
7. **PD33 — Trigger instance lifecycle.** Create validates config against the
   definition's config schema and requires an ACTIVE connection; the instance is born
   enabled with a stable `trg_` id. `disable` stops firing (poll state retained),
   `enable` resumes, `delete` is permanent. Deleting a connection deletes its
   instances; a connection leaving ACTIVE (EXPIRED/DISCONNECTED) pauses its instances
   automatically, and reconnect resumes them. Instances are cheap and unshared —
   rolai's ref-counting stays consumer-side, as today.
8. **PD34 — Polling mechanics.** Each instance keeps a per-instance watermark
   (provider-side timestamp + seen-id guard). The first poll after create establishes
   the baseline and delivers nothing historical; each subsequent poll emits one
   `trigger.event` per new record, exactly once. Records arriving while an instance is
   disabled are skipped on re-enable (disable means stop, not buffer — the watermark
   resets to now). A poll run never overlaps itself; poll failures (provider errors,
   rate limits per PD21 normalization) log and wait for the next tick without killing
   the schedule.
9. **PD35 — Phase 3 trigger set.** `outlook-message-received` (config: `folderId`,
   default Inbox — the direct counterpart of the Membrane sample; polls Graph messages
   newer than the watermark, payload: curated message fields — id, subject, from,
   receivedDateTime, bodyPreview, folder id) and `hubspot-contact-created` (polls CRM
   contacts by `createdate`; payload: contact id + properties). One trigger per
   provider proves the format is not Outlook-shaped.
10. **PD36 — Proactive refresh scheduler.** Every 60s, ACTIVE connections whose access
    token expires within the next 10 minutes are claimed and refreshed (jittered).
    A permanent refusal (`invalid_grant` and kin) marks the connection EXPIRED and
    emits `connection.expired` through the outbox; transient failures (network, 5xx)
    just retry on later scans while the token still lives. Per-connection locking
    ensures the scheduler and PD18's request-path refresh never race (one refresh at a
    time; the loser reuses the winner's result). PD18 stays as the backstop for
    anything the scheduler misses. **Every** ACTIVE→EXPIRED transition — scheduler or
    request path — emits `connection.expired` exactly once per transition.
11. **PD37 — Reconciliation job.** Every 6 hours (configurable), ACTIVE connections
    are verified against their provider with a lightweight authenticated call (the
    definition's `userInfoUrl`); a connection the provider no longer honors (token
    revoked behind our back, refresh also dead) becomes EXPIRED + `connection.expired`.
    Runs are jittered and spread out to respect provider rate limits.
12. **PD38 — Phase 2 carry-forwards land here** (review notes): (a) user tokens with
    `exp − iat` > 24h are rejected as unauthorized, and the SDK refuses to mint them —
    `iat` finally has a job; (b) the OrgOrUser middleware surfaces infrastructure
    failures as 500, no longer conflating them with 401; (c) the
    `EncryptPlaintextClientSecrets` boot backfill logs a success line with the row
    count; (d) `/metrics` gains a connections-by-status gauge (EXPIRED visible at a
    glance) plus outbox and delivery instrumentation (PD24 extended).

---

## Slice 1 — The catalog announces events: trigger definitions in the format

The `triggers` section reserved by PD13 becomes real: definitions carry config and
payload schemas, and the API rolai calls `getTriggerDefinitions` exists — without the
flow-naming hack.

- [x] Consumer can list trigger definitions for a provider or integration, each with slug, name, description, config schema, payload schema, and ingestion mode, cursor-paginated
- [x] Consumer can fetch a single trigger definition by slug with the same detail
- [x] The Outlook definition ships `outlook-message-received` (folderId config, default Inbox) and the Hubspot definition ships `hubspot-contact-created`
- [x] A definition with an invalid triggers section fails boot with a message naming the file, trigger, and field — including a trigger missing a config or payload schema
- [x] A definition declaring push ingestion fails boot with a clear not-supported-yet message
- [x] Listing trigger definitions for an unknown provider slug or another organization's integration returns not-found

## Slice 2 — Subscriptions live: trigger instance lifecycle

Create/enable/disable/delete bound to Connections — Composio's instance surface with
stable `trg_` ids and none of Membrane's instanceKey ceremony.

- [x] Consumer can create a trigger instance with connectionId + trigger slug + config and receives a `trg_`-prefixed id with status ACTIVE
- [x] Config is validated against the definition's config schema; invalid config is rejected with a validation error and no instance is created
- [x] Creating an instance against a connection that is not ACTIVE is rejected with a status-explaining validation error
- [x] Consumer can list trigger instances filtered by connectionId or userId, cursor-paginated, and fetch one by id showing status, trigger slug, connection, and config
- [x] Consumer can disable an instance (status DISABLED, it stops firing) and re-enable it (status ACTIVE)
- [x] Consumer can delete an instance permanently — it is not-found afterwards
- [x] Deleting a connection deletes its trigger instances
- [x] Instance operations against another organization's instance or connection return not-found

## Slice 3 — The signed channel: endpoint, outbox, delivery

The new security surface, proven before anything real rides on it: an endpoint with a
`whsec_` secret, events persisted before delivery, Standard Webhooks signatures,
retries, and manual redelivery.

- [x] Consumer can set its organization's webhook endpoint URL; the signing secret (`whsec_`-prefixed) is returned exactly once, at creation
- [x] Fetching the endpoint shows URL, secret prefix, and created date — never the full secret, which is also unrecoverable from a database dump
- [x] Consumer can request a test event and receives a signed `webhook.test` delivery at the endpoint
- [x] Every delivery carries `webhook-id`, `webhook-timestamp`, and `webhook-signature` headers, and the signature verifies against the body with the endpoint secret
- [x] Events are persisted before the first delivery attempt — an event accepted just before a server restart is still delivered after the restart
- [x] A delivery answered with non-2xx or a timeout is retried on the backoff schedule; the retried delivery carries the same `webhook-id` and body as the original
- [x] When retries are exhausted the event shows delivery status FAILED; consumer can list events with their delivery state and re-queue one for redelivery
- [x] Setting the endpoint URL to a value that is not an absolute http(s) URL is rejected with a validation error
- [x] An organization without a configured endpoint accumulates no failed deliveries — events are persisted and marked undeliverable-without-endpoint, and requesting a test event is rejected with a validation error
- [x] Consumer can rotate the endpoint secret: the new secret is returned exactly once, deliveries during the overlap window carry signatures from both secrets, and after the window only the new secret verifies
- [x] Every delivery attempt writes a log entry with event id, event type, attempt number, response status, and duration

## Slice 4 — Triggers fire: polling ingestion end-to-end

The Membrane sample, replaced: a new Outlook message becomes a signed `trigger.event`
at the consumer's endpoint — once, and only for records after subscription.

- [x] A new provider record arriving after instance creation (e.g. a new Outlook message in the configured folder) produces a signed `trigger.event` delivery whose data carries the instance id, connection id, user id, trigger slug, and a payload conforming to the definition's payload schema
- [x] Records that existed before the instance was created produce no events (baseline poll delivers nothing historical)
- [x] The same provider record never produces a second `trigger.event` across consecutive polls
- [x] A `hubspot-contact-created` instance fires when a new contact appears (proves the poll engine is definition-driven, not Outlook-specific)
- [x] A disabled instance stops producing events; after re-enable, records that arrived while disabled are skipped and new records fire again
- [x] When an instance's connection leaves ACTIVE, polling pauses automatically and produces no failed-poll noise; completing a reconnect resumes it
- [x] A failing poll (provider error or rate limit) writes a log entry and does not stop the schedule — the next tick runs normally
- [x] Two instances on the same connection with different configs (e.g. different folders) fire independently

## Slice 5 — Tokens heal themselves: refresh scheduler, EXPIRED, reconciliation

The success criterion from discovery, verbatim: expiry is invisible while refresh
works, and a signed event arrives within minutes when it does not.

- [x] An ACTIVE connection whose access token nears expiry is refreshed in the background before it expires — a consumer executing right after expiry-time gets a normal success without triggering the request-path refresh
- [x] A rotated refresh token returned during a scheduled refresh replaces the stored one
- [x] A permanent refresh refusal (e.g. revoked refresh token) marks the connection EXPIRED and delivers a signed `connection.expired` event carrying connection id, user id, integration id, provider slug, and reason
- [x] A transient refresh failure (network error, provider 5xx) does not mark the connection EXPIRED — it is retried on a later scan
- [x] A request-path refresh failure (PD18) also delivers `connection.expired`; a connection's ACTIVE→EXPIRED transition emits exactly one event no matter which path detected it
- [x] A concurrent tool execution and scheduled refresh on the same connection perform only one token refresh between them, and the execution succeeds
- [x] The reconciliation job detects an ACTIVE connection whose token the provider has revoked (refresh also refused) and marks it EXPIRED with a `connection.expired` event
- [x] Reconnecting an EXPIRED connection restores it to ACTIVE with the same id, and both the refresh scheduler and its paused trigger instances resume

## Slice 6 — The SDK verifies: webhooks and triggers in TypeScript

The one-line verifier discovery promised, plus the trigger surface rolai migrates onto.

- [x] `beecon.webhooks.verify({ payload, headers, secret })` returns a typed event (`trigger.event` | `connection.expired` | `webhook.test`) for a valid delivery
- [x] Verification throws a typed error for a wrong signature, a tampered payload, or a timestamp outside the ±5-minute tolerance
- [x] Verification accepts deliveries signed during a secret-rotation overlap when the consumer holds either secret
- [x] `beecon.triggers.listDefinitions({ providerSlug | integrationId })` and `.getDefinition(slug)` return typed definitions with config and payload schemas
- [x] `beecon.triggers.create({ connectionId, slug, config })`, `.list(...)`, `.get(id)`, `.enable(id)`, `.disable(id)`, `.delete(id)` cover the instance lifecycle
- [x] `beecon.webhookEndpoint.set({ url })`, `.get()`, `.rotateSecret({ overlapHours? })`, `.sendTest()` manage the endpoint; `beecon.events.list(filters)` and `.redeliver(id)` cover the outbox
- [x] The SDK never writes the webhook secret to logs, errors, or serialized output (parity with the API-key and signing-secret guarantees)
- [x] The quickstart is extended with an end-to-end receive-and-verify walkthrough: register endpoint, create a trigger instance, verify and handle the delivery in an Express/Next handler, deduplicate on the event id

## Slice 7 — Debts paid, dials added: carry-forwards and operability

The Phase 2 review notes, plus the instrumentation that makes background processing
observable before anyone depends on it.

- [x] A user token whose lifetime (`exp − iat`) exceeds 24 hours is rejected as unauthorized, and the SDK refuses to mint one with a clear error
- [x] An infrastructure failure (e.g. database down) during org-or-user auth surfaces as a 500 — not a 401
- [x] The client-secret boot backfill logs a success line including the number of rows encrypted
- [x] `/metrics` exposes a connections-by-status gauge (INITIATED/ACTIVE/EXPIRED/DISCONNECTED counts)
- [x] `/metrics` exposes outbox depth and oldest-pending-event age, delivery attempt outcomes by event type and result, trigger poll runs and events emitted, and scheduled-refresh outcomes
- [x] Background workers stop cleanly on shutdown: in-flight deliveries and refreshes finish or release their claims, and nothing is lost or double-processed on restart

---

## API Shape (indicative)

```
Trigger catalog
GET  /api/v1/trigger-definitions?providerSlug=|integrationId=&cursor=&limit=   (org | user token)
     -> { items: [{ slug, name, description, configSchema, payloadSchema,
                    ingestion: "poll", provider: { slug, name, logo } }], nextCursor }
GET  /api/v1/trigger-definitions/{slug}                                        (org)

Trigger instances
POST /api/v1/trigger-instances               (org)  { connectionId, triggerSlug, config }
                                             -> 201 { id: "trg_...", status: "ACTIVE" }
GET  /api/v1/trigger-instances?connectionId=|userId=&cursor=&limit=            (org)
GET  /api/v1/trigger-instances/{trgId}       (org)
POST /api/v1/trigger-instances/{trgId}/disable | /enable                       (org)
DELETE /api/v1/trigger-instances/{trgId}     (org)  -> 204

Webhook endpoint + events
PUT  /api/v1/webhook-endpoint                (org)  { url }
                                             -> { id: "wep_...", url, secret: "whsec_..." (creation only) }
GET  /api/v1/webhook-endpoint                (org)  -> { id, url, secretPrefix, createdAt }
POST /api/v1/webhook-endpoint/rotate-secret  (org)  { overlapHours? } -> { secret: "whsec_..." }
POST /api/v1/webhook-endpoint/test           (org)  -> 202
GET  /api/v1/events?type=&deliveryStatus=&cursor=&limit=                       (org)
     -> { items: [{ id: "evt_...", type, createdAt, deliveryStatus, attempts, lastAttemptAt }], nextCursor }
POST /api/v1/events/{evtId}/redeliver        (org)  -> 202

Delivery (Beecon -> consumer endpoint)
POST {endpoint.url}
  webhook-id:        evt_...
  webhook-timestamp: 1760000000
  webhook-signature: v1,<base64> [v1,<base64-with-old-secret-during-rotation>]
  body: { "id": "evt_...", "type": "trigger.event" | "connection.expired" | "webhook.test",
          "createdAt": "...", "data": { ... } }
  signed content: {id}.{timestamp}.{raw body}   (HMAC-SHA256, secret whsec_...)
  success = HTTP 2xx within 10s; retries: ~0s, 5s, 5m, 30m, 2h, 5h, 10h, 14h, 20h, 24h (jittered)

Trigger instance statuses: ACTIVE | DISABLED
Event delivery statuses:   PENDING | DELIVERED | FAILED | NO_ENDPOINT
```

## Out of Scope (Phase 3)

- Push trigger ingestion (MS Graph change notifications, Hubspot app webhooks) — the
  format field exists (PD28), the implementation does not
- Service-bus delivery adapter (Phase 5) — delivery stays behind a port so the bus
  adapter adds an adapter, not core changes
- Multiple webhook endpoints per organization, per-endpoint event-type filters,
  endpoint auto-disable on persistent failure (Phase 4+)
- Trigger definitions beyond PD35's two; trigger param autocomplete (Composio's
  trigger-mapping service equivalent)
- Event/outbox retention configuration (lands with log retention, Phase 4)
- Strict per-connection ordering guarantees — ordering is best-effort (PD30)
- Historical backfill/replay of provider records predating an instance
- Admin UI surfacing of triggers, events, and schedulers (Phase 4)

## Technical Context

- **Stack (binding):** the one Go binary grows its first background workers — outbox
  dispatcher, trigger poller, refresh scheduler, reconciliation — as in-process
  goroutines with database-lease claims (PD29). Still no queue, no cron, no new
  runtime; two instances of the binary must remain safe.
- **Ids (binding):** new prefixes this phase: `trg_` (trigger instance), `evt_`
  (outbox event), `wep_` (webhook endpoint). Secret value prefix `whsec_` (Standard
  Webhooks convention). Trigger definitions are slug-addressed like tools (PD14).
- **Boundaries:** per `.claude/BOUNDARIES.md` — trigger definitions in `catalog/`;
  TriggerInstance + poll ingestion in `triggers/` (depends on connections, catalog);
  Outbox + WebhookDelivery + reverse messages in `delivery/` (depends on access,
  organizations — triggers/connections hand events to delivery through a port);
  refresh scheduler + reconciliation in `connections/`; endpoint secret storage
  follows `access/`'s WebhookSigningSecret ownership; carry-forward fixes in
  `access/` and `app/`.
- **Existing crypto:** the endpoint secret is encrypted with the Phase 1 AES-256-GCM
  vault key — no second scheme (same rationale as PD20's HS256 secret).
- **Envelope (binding):** PD5/PD6 stand untouched; webhook deliveries use the PD32
  event envelope, which is a new surface, not a change to the execute envelope.
- **Reference shape:** `temp/outlook-message-received-trigger.yaml` — the polling
  cadence (60s), folderId config-with-default, and consumer POST body
  (`{data: {trigger_id, ...}}`) it implements are what Slice 4 replaces first-class.
- **New config:** `BEECON_REFRESH_LEAD` (default 10m), `BEECON_REFRESH_SCAN_INTERVAL`
  (default 60s), `BEECON_RECONCILE_INTERVAL` (default 6h), `BEECON_DELIVERY_TIMEOUT`
  (default 10s), `BEECON_POLL_MIN_INTERVAL` (default 30s).
- **Metrics:** PD24's `/metrics` endpoint extended (Slice 7), never a second endpoint.
- Risk level: HIGH

---

- [x] Reviewed <!-- approved by developer ("go ahead"), 2026-07-14; four PDs confirmed via Q&A (PD27/PD28/PD30/PD31) -->
