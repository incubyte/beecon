/**
 * readCsrfToken reads the non-HttpOnly beecon_csrf cookie the server sets
 * alongside the session cookie on login (PD52) — the SPA's own half of the
 * double-submit CSRF scheme. It lives in its own module (not api-client.ts
 * or auth.ts) so both can import it without creating a circular dependency:
 * api-client.ts (Slice 3) reads it to attach the X-CSRF-Token header on
 * every mutating request, and auth.ts re-exports it for lib/auth.ts's own
 * existing callers/tests.
 */
export function readCsrfToken(): string | null {
  const match = document.cookie.match(/(?:^|;\s*)beecon_csrf=([^;]*)/);
  return match ? decodeURIComponent(match[1]) : null;
}
