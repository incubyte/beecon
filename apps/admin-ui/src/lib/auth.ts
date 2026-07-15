import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import { readCsrfToken } from "./csrf";
import { queryClient, queryKeys } from "./query";
import { clearSessionExpiredMidWork, resolveSessionExpiredMidWork } from "./session-state";

export { readCsrfToken, resolveSessionExpiredMidWork };

/**
 * SessionOperator is GET /api/v1/auth/me's response shape (Phase 5 Slice 1,
 * PD49/PD55): the authenticated operator's own identity — never a password
 * or its hash.
 */
export interface SessionOperator {
  id: string;
  email: string;
}

/**
 * useSession probes GET /api/v1/auth/me: 200 means the same-origin
 * beecon_session cookie authenticated the request as this operator; 401
 * (surfaced by react-query as an error, not a thrown exception the caller
 * has to catch) means there is no session, or it expired/was revoked. The
 * SPA holds no admin key, and no credential of any kind, in JS memory —
 * this replaces PD39's in-memory admin-key store entirely (PD55).
 * routes/__root.tsx's guard reads this to choose LoginScreen vs the
 * authenticated shell.
 */
export function useSession(): UseQueryResult<SessionOperator> {
  return useQuery({
    queryKey: queryKeys.auth.me(),
    queryFn: () => apiClient.get<SessionOperator>("/auth/me"),
    retry: false,
  });
}

/**
 * useReauthRequired reports whether a console API call returned 401 while
 * the operator was already authenticated (Slice 5, lib/session-state.ts):
 * true means the session expired/was revoked mid-work, and AppShell should
 * render ReauthModal over the current page rather than routes/__root.tsx
 * unmounting the shell in favor of LoginScreen. Distinct from the initial
 * unauthenticated load (useSession's own 401, never touches this flag) and
 * an explicit sign-out (useSignOut clears this flag directly and heads to
 * LoginScreen via the normal auth.me invalidation).
 */
export function useReauthRequired(): boolean {
  const { data } = useQuery({
    queryKey: queryKeys.auth.reauthRequired(),
    queryFn: () => false,
    initialData: false,
    staleTime: Infinity,
  });
  return data ?? false;
}

/**
 * cachedOperatorEmail reads the email useSession's last successful `/auth/me`
 * response cached, without subscribing to it (a plain read, not a hook) — so
 * ReauthModal can prefill/display the known operator identity for a
 * password-only re-authentication (Slice 5) instead of asking for the email
 * again. undefined only if no session was ever established this page load
 * (shouldn't happen for a mid-work reauth, since it can only fire after a
 * successful `/auth/me` populated this cache in the first place).
 */
export function cachedOperatorEmail(): string | undefined {
  return queryClient.getQueryData<SessionOperator>(queryKeys.auth.me())?.email;
}

/**
 * useSignOut ends the operator's session (Phase 5 Slice 2, PD49 AC1): it
 * POSTs /api/v1/auth/logout — which revokes the session server-side and
 * clears both PD52 cookies — then marks the `auth.me` probe stale either
 * way, so routes/__root.tsx falls back to LoginScreen on the next render
 * even if the logout call itself fails (e.g. the session was already
 * expired/revoked; signing out must never leave the operator stuck in the
 * shell). Used by the top bar's "Sign out" button and the command palette's
 * "Sign out" entry.
 */
export function useSignOut(): () => void {
  const queryClient = useQueryClient();
  return () => {
    void apiClient
      .post("/auth/logout")
      .catch(() => undefined)
      .finally(() => {
        // An explicit sign-out always heads to LoginScreen — clear any
        // pending mid-work reauth flag first so ReauthModal never renders
        // for an instant over a shell that's about to unmount anyway.
        clearSessionExpiredMidWork();
        void queryClient.invalidateQueries({ queryKey: queryKeys.auth.me() });
      });
  };
}

