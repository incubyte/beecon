# Quickstart: connect a user and run their first tool

This walks the whole popup connect flow end-to-end with `@beecon/sdk`: a
server-side `initiate` call, a popup window that carries the end user through
Beecon's own connect page and the provider's consent screen, the
`redirectUri` round-trip back to your app, and the first tool execution.

## 1. Install and construct the client

```bash
npm install @beecon/sdk
```

```ts
// server/beecon.ts
import { Beecon, type BeeconClient } from '@beecon/sdk';

// Type consumers against BeeconClient, not the Beecon class, so tests can
// inject a vi.fn()-built double (every sub-api BeeconClient declares must be
// present, or `satisfies BeeconClient` fails to compile):
//
//   const beecon: BeeconClient = {
//     users: { create: vi.fn() },
//     integrations: { list: vi.fn(), getExpectedParams: vi.fn() },
//     connections: {
//       initiate: vi.fn(), get: vi.fn(), list: vi.fn(),
//       disable: vi.fn(), delete: vi.fn(), reconnect: vi.fn(),
//     },
//     tools: { list: vi.fn(), get: vi.fn(), execute: vi.fn() },
//     logs: { list: vi.fn() },
//     userTokens: { create: vi.fn() },
//     files: { upload: vi.fn() },
//   };
export const beecon: BeeconClient = new Beecon({
  apiKey: process.env.BEECON_API_KEY!, // beecon_sk_...
  baseUrl: process.env.BEECON_BASE_URL!, // e.g. https://beecon.example.com
});
```

## 2. Create a user (once, server-side)

```ts
const user = await beecon.users.create({
  name: 'Ada Lovelace',
  externalId: 'app-user-42', // your own id, optional
});
// user.id is a user_-prefixed id — store it alongside your own user record
```

## 3. List integrations and initiate a connection (server-side)

The end user picks an integration (e.g. Outlook) in your UI. Your server
calls `initiate` with a `redirectUri` on your own domain — it must be on the
organization's allowed-redirect-uri list (set by your installation admin) or
`initiate` rejects it.

```ts
const integrations = await beecon.integrations.list();
const outlook = integrations.find((i) => i.providerSlug === 'outlook')!;

const initiated = await beecon.connections.initiate({
  userId: user.id,
  integrationId: outlook.id,
  redirectUri: 'https://app.example.com/integrations/connected',
});
// initiated -> { id: "conn_...", status: "INITIATED", redirectUrl: "https://beecon.../connect/..." }
```

Return `initiated.redirectUrl` (and `initiated.id`) to your browser client.

## 4. Open the popup (browser-side)

Open `redirectUrl` in a popup synchronously, in the same event-loop tick as
the user's click — otherwise popup blockers will kill it:

```ts
// client/connect.ts
function openConnectPopup(redirectUrl: string): Window {
  const popup = window.open(
    redirectUrl,
    'beecon-connect',
    'width=480,height=720',
  );
  if (!popup) {
    throw new Error('Popup was blocked — open it directly in response to a user gesture.');
  }
  return popup;
}
```

Beecon's connect page (served from `redirectUrl`) shows the provider's name
and logo with a Connect action. Choosing Connect sends the popup to the
provider's consent screen; after the user consents (or denies), Beecon's
callback exchanges the code for tokens and redirects the popup to **your**
`redirectUri`, carrying the round-trip result as query parameters:

```
https://app.example.com/integrations/connected?connectionId=conn_...&status=success
https://app.example.com/integrations/connected?connectionId=conn_...&status=error
```

## 5. Detect completion (browser-side)

Poll the popup's `closed` state and, once it navigates to your own origin,
read the query string it landed on. A simple polling pattern (works across
Chrome-extension and regular web contexts, unlike `postMessage`, since the
popup navigates through the provider's origin first):

```ts
// client/connect.ts
function waitForConnectResult(popup: Window): Promise<{ connectionId: string; status: string }> {
  return new Promise((resolve, reject) => {
    const interval = setInterval(() => {
      let redirectedUrl: URL | null = null;
      try {
        // Throws while the popup is still on the provider's / Beecon's
        // origin (cross-origin); succeeds once it's back on your origin.
        redirectedUrl = new URL(popup.location.href);
      } catch {
        // still mid-flow
      }

      if (popup.closed) {
        clearInterval(interval);
        reject(new Error('Connect popup was closed before completing.'));
        return;
      }

      if (redirectedUrl && redirectedUrl.pathname === '/integrations/connected') {
        clearInterval(interval);
        popup.close();
        resolve({
          connectionId: redirectedUrl.searchParams.get('connectionId')!,
          status: redirectedUrl.searchParams.get('status')!,
        });
      }
    }, 500);
  });
}
```

Your `/integrations/connected` page itself can be a no-op (or a "you're
connected!" message) — the polling loop above already extracted the result
before the popup closes. If you'd rather drive this from the redirect page
instead of polling, have that page call `window.opener.postMessage(...)`
with the same `connectionId`/`status` pair and close itself; either approach
works since the round-trip is just a URL your app controls.

## 6. Confirm the connection is active (server-side)

```ts
const { connectionId, status } = await waitForConnectResult(popup);

if (status !== 'success') {
  throw new Error('User did not complete the connect flow.');
}

const connection = await beecon.connections.get(connectionId);
// connection -> { id, status: "ACTIVE", providerSlug: "outlook", userId, createdAt,
//                 account: { email, displayName } }
// Never tokens — those stay encrypted in Beecon's vault.
```

## 7. Execute the first tool

```ts
const result = await beecon.tools.execute('outlook-list-messages', {
  userId: user.id,
  connectionId: connection.id,
  arguments: { top: 10 },
});

if (result.successful) {
  console.log(result.data); // the mailbox messages
} else {
  // Tool-level failure: invalid args, non-ACTIVE connection, or an upstream
  // provider error. This is a returned value, not a thrown exception —
  // build your retry logic around `result.error`.
  console.error(result.error);
}
```

## Error handling

Only platform-level failures (bad API key, unknown connection/tool,
cross-organization access, validation errors on the request itself) throw:

```ts
import { BeeconApiError } from '@beecon/sdk';

try {
  await beecon.connections.get('conn_does_not_exist');
} catch (err) {
  if (err instanceof BeeconApiError) {
    console.error(err.status, err.code, err.message); // e.g. 404 not_found "..."
  }
  throw err;
}
```

Tool-level outcomes never throw — they come back as
`{ successful, error, data }` from `tools.execute`, as shown above.

An upstream rate limit that survives Beecon's own retry surfaces as a typed
`RateLimitedError` (a `BeeconApiError` subclass) carrying `retryAfter` in
seconds:

```ts
import { RateLimitedError } from '@beecon/sdk';

try {
  await beecon.tools.execute('hubspot-list-contacts', {
    userId: user.id,
    connectionId: connection.id,
    arguments: {},
  });
} catch (err) {
  if (err instanceof RateLimitedError) {
    console.error(`rate limited, retry after ${err.retryAfter}s`);
  }
  throw err;
}
```

## The browser-token connect flow

The popup flow above works when your server already knows which user is
connecting. When the connect action starts entirely in the browser (a
Chrome-extension popup, an embedded widget with no server round-trip for the
click itself), mint a short-lived user token instead of calling `initiate`
with a server API key.

Configure the client with a signing secret (issued once, server-side, via the
admin signing-secrets endpoint) alongside the API key:

```ts
// server/beecon.ts
export const beecon: BeeconClient = new Beecon({
  apiKey: process.env.BEECON_API_KEY!,
  baseUrl: process.env.BEECON_BASE_URL!,
  signingSecret: {
    id: process.env.BEECON_SIGNING_SECRET_ID!, // usk_...
    secret: process.env.BEECON_SIGNING_SECRET!,
  },
});
```

Mint a token for the signed-in user and hand it to the browser — minting is
entirely local (no network call), so it never throws `BeeconApiError`, only
`MissingSigningSecretError` if `signingSecret` was never configured:

```ts
const userToken = beecon.userTokens.create({ userId: user.id }); // default 2h expiry
// userToken -> { token: "eyJhbGciOiJIUzI1NiIs...", expiresAt: "2026-07-13T18:00:00.000Z" }
```

The browser calls the same Beecon API with `Authorization: Bearer <token>`
instead of the server API key. The token's surface is intentionally narrow
(PD20): list integrations, initiate a connection (the `userId` is always
taken from the token, never the request body), and list/get/reconnect the
token's **own** connections. Everything else — disabling or deleting a
connection, uploading a file, rotating keys — stays server-key-only.

## Connecting Hubspot

Hubspot connects through the exact same `initiate` → popup → `redirectUri`
round-trip as Outlook above — only the integration and tool slugs differ:

```ts
const integrations = await beecon.integrations.list();
const hubspot = integrations.find((i) => i.providerSlug === 'hubspot')!;

const initiated = await beecon.connections.initiate({
  userId: user.id,
  integrationId: hubspot.id,
  redirectUri: 'https://app.example.com/integrations/connected',
});
// open the popup at initiated.redirectUrl exactly as in step 4 above
```

## Paging through a list tool

`hubspot-list-contacts` (and any tool whose mapping declares pagination)
accepts canonical `pageSize`/`cursor` arguments and returns the next page's
cursor as a top-level `nextCursor` on the execution result — separate from
`data`, which stays whatever shape the tool's own output schema declares:

```ts
let cursor: string | undefined;
const allContacts: unknown[] = [];

do {
  const result = await beecon.tools.execute('hubspot-list-contacts', {
    userId: user.id,
    connectionId: connection.id,
    arguments: { pageSize: 50, cursor },
  });
  if (!result.successful) {
    throw new Error(result.error?.message);
  }
  allContacts.push(...(result.data as { results: unknown[] }).results);
  cursor = result.nextCursor;
} while (cursor);
```

You can also browse the catalog itself with cursor pagination, the same
convention every Beecon list API uses:

```ts
const page = await beecon.tools.list({ providerSlug: 'hubspot', limit: 20 });
console.log(page.items.map((tool) => tool.slug));
if (page.nextCursor) {
  const nextPage = await beecon.tools.list({ providerSlug: 'hubspot', cursor: page.nextCursor, limit: 20 });
}
```

## Uploading a file into a tool call

Upload a file first, then pass its returned `id` as the file-typed argument
a tool's mapping expects (e.g. `hubspot-upload-file`):

```ts
import { readFile } from 'node:fs/promises';

const bytes = await readFile('./invoice.pdf');
const uploaded = await beecon.files.upload({
  fileName: 'invoice.pdf',
  mimeType: 'application/pdf',
  content: bytes,
});
// uploaded -> { id: "file_...", name: "invoice.pdf", mimeType: "application/pdf",
//               size: 84213, downloadUrl: "https://.../api/v1/files/file_.../download" }

const result = await beecon.tools.execute('hubspot-upload-file', {
  userId: user.id,
  connectionId: connection.id,
  arguments: { file: uploaded.id },
});
```

File upload is org-key-only (never reachable with a browser user token) —
route the upload through your server, then hand the resulting `id` to the
browser if the tool call itself happens client-side.

## Receiving and verifying webhooks

Triggers and connection lifecycle events (PD32) arrive at your own HTTP
endpoint as signed deliveries — Standard Webhooks (PD27). This walks
registering the endpoint, creating a trigger instance, and verifying and
handling a delivery in an Express (or Next.js Route Handler) handler.

### 1. Register your endpoint (once, server-side)

```ts
const endpoint = await beecon.webhookEndpoint.set({
  url: 'https://app.example.com/webhooks/beecon',
});
// endpoint -> { id: "wep_...", url, secret: "whsec_..." (present only on
//               this first call), createdAt }

// Store endpoint.secret somewhere your webhook handler can read it
// (an environment variable, a secrets manager) — it is returned exactly
// once. A later `webhookEndpoint.get()` only ever returns a secretPrefix.
```

Prove the channel works before anything real depends on it:

```ts
await beecon.webhookEndpoint.sendTest(); // 202 — a webhook.test event lands shortly at your endpoint
```

### 2. Create a trigger instance (server-side)

```ts
const definitions = await beecon.triggers.listDefinitions({ providerSlug: 'outlook' });
const messageReceived = definitions.items.find((d) => d.slug === 'outlook-message-received')!;

const instance = await beecon.triggers.create({
  connectionId: connection.id, // an ACTIVE connection (see step 6 above)
  slug: messageReceived.slug,
  config: { folderId: 'Inbox' },
});
// instance -> { id: "trg_...", status: "ACTIVE" }
```

### 3. Verify and handle a delivery (Express)

Read the **raw** request body — `webhooks.verify` signs over the exact bytes
Beecon sent, so a body-parser that re-serializes JSON will break signature
verification:

```ts
// server/webhooks.ts
import express from 'express';
import { webhooks, WebhookVerificationError } from '@beecon/sdk';

const app = express();
const seenEventIds = new Set<string>(); // replace with persistent storage

app.post(
  '/webhooks/beecon',
  express.raw({ type: 'application/json' }), // keeps req.body as a Buffer
  (req, res) => {
    let event;
    try {
      event = webhooks.verify({
        payload: req.body.toString('utf8'),
        headers: req.headers as Record<string, string | string[] | undefined>,
        secret: process.env.BEECON_WEBHOOK_SECRET!, // the whsec_... from step 1
      });
    } catch (err) {
      if (err instanceof WebhookVerificationError) {
        console.error('rejected webhook delivery:', err.reason, err.message);
        return res.status(400).send('invalid signature');
      }
      throw err;
    }

    // The evt_ id is stable across retries and manual redeliveries (PD32) —
    // deduplicate on it before acting on the event.
    if (seenEventIds.has(event.id)) {
      return res.status(200).send('already processed');
    }
    seenEventIds.add(event.id);

    switch (event.type) {
      case 'trigger.event':
        console.log('new record', event.data.triggerSlug, event.data.payload);
        break;
      case 'connection.expired':
        console.log('connection needs reconnect', event.data.connectionId, event.data.reason);
        break;
      case 'webhook.test':
        console.log('webhook.test received — the channel works');
        break;
    }

    res.status(200).send('ok');
  },
);
```

The same `webhooks.verify` call works in a Next.js Route Handler — read
`await request.text()` for the raw payload and pass `request.headers`
(a WHATWG `Headers` instance) straight through:

```ts
// app/api/webhooks/beecon/route.ts
import { webhooks, WebhookVerificationError } from '@beecon/sdk';

export async function POST(request: Request) {
  const payload = await request.text();
  try {
    const event = webhooks.verify({
      payload,
      headers: request.headers,
      secret: process.env.BEECON_WEBHOOK_SECRET!,
    });
    // ...same event.type switch and webhook-id dedupe as above
    return new Response('ok', { status: 200 });
  } catch (err) {
    if (err instanceof WebhookVerificationError) {
      return new Response('invalid signature', { status: 400 });
    }
    throw err;
  }
}
```

`webhooks.verify` also accepts `secrets: string[]` instead of `secret` —
pass both the new and old secret during a rotation overlap window and
verification succeeds against either one:

```ts
webhooks.verify({
  payload,
  headers: request.headers,
  secrets: [process.env.BEECON_WEBHOOK_SECRET!, process.env.BEECON_WEBHOOK_SECRET_PREVIOUS!],
});
```

### 4. Manage delivery history

```ts
const events = await beecon.events.list({ deliveryStatus: 'FAILED' });
for (const event of events.items) {
  await beecon.events.redeliver(event.id); // 202 — re-queues the same evt_ id and body
}
```
