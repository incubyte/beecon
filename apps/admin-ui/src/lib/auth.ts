import { useSyncExternalStore } from "react";

/**
 * The PD39 admin-key gate: the key lives in this module's own variable for
 * the tab's lifetime only — never localStorage, sessionStorage, or a
 * cookie. Reloading the tab or opening a new tab starts a fresh JS module
 * instance with `adminKey` back at `null`, so the gate screen shows again
 * (Slice 1, AC3/AC4). Do not add persistence here; that is the whole point
 * of PD39's deferred-auth posture — see README.md.
 */
let adminKey: string | null = null;
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

export function getAdminKey(): string | null {
  return adminKey;
}

export function setAdminKey(key: string): void {
  adminKey = key;
  notify();
}

export function clearAdminKey(): void {
  adminKey = null;
  notify();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** useAdminKey re-renders its caller whenever the in-memory key changes
 * (login, sign-out, or an api-client 401 auto-clear). */
export function useAdminKey(): string | null {
  return useSyncExternalStore(subscribe, getAdminKey, getAdminKey);
}

export function useIsAuthenticated(): boolean {
  return useAdminKey() !== null;
}
