import { queryClient, queryKeys } from "./query";

/**
 * session-state.ts holds the "session expired mid-work" flag as its own tiny
 * module — mirroring csrf.ts's own rationale — so both api-client.ts (which
 * sets it, Slice 5) and auth.ts (which reads it via useReauthRequired and
 * clears it, Slice 5) can import it without creating a circular dependency
 * between the two (auth.ts already imports api-client.ts's apiClient).
 *
 * The flag itself lives as react-query cache data under
 * queryKeys.auth.reauthRequired() rather than a plain module-level variable,
 * so components can react to it with the same useQuery/setQueryData
 * mechanism the rest of the app already uses — no extra state-management
 * library, no context provider to wire up.
 */

/**
 * markSessionExpiredMidWork flags reauthRequired true (api-client.ts's
 * default onUnauthorized): called only for a 401 from a console API call
 * other than the `/auth/me` probe itself, so the still-cached auth.me data
 * is left untouched — AppShell stays mounted underneath ReauthModal, and the
 * operator's in-progress page state (route, unsubmitted form fields) is
 * never lost to a hard bounce to LoginScreen.
 */
export function markSessionExpiredMidWork(): void {
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), true);
}

/**
 * clearSessionExpiredMidWork resets the flag without refetching anything —
 * used by an explicit sign-out (useSignOut), which is heading to LoginScreen
 * anyway and must never leave a stale ReauthModal mounted underneath it.
 */
export function clearSessionExpiredMidWork(): void {
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
}

/**
 * resolveSessionExpiredMidWork is ReauthModal's own success handler: clears
 * the flag, then refetches every currently-active query so the page
 * underneath resumes with fresh data under the newly re-authenticated
 * session — "resuming where they were", not a stale read left over from
 * before the session expired.
 */
export function resolveSessionExpiredMidWork(): void {
  clearSessionExpiredMidWork();
  void queryClient.refetchQueries({ type: "active" });
}
