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
// inject a vi.fn()-built double:
//
//   const beecon: BeeconClient = {
//     users: { create: vi.fn() },
//     integrations: { list: vi.fn() },
//     connections: { initiate: vi.fn(), get: vi.fn() },
//     tools: { execute: vi.fn() },
//     logs: { list: vi.fn() },
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
