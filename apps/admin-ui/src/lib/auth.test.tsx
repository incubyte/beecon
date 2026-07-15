import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";

import { cachedOperatorEmail, readCsrfToken, useReauthRequired, useSession, useSignOut } from "./auth";
import { queryClient as singletonQueryClient, queryKeys } from "./query";
import { markSessionExpiredMidWork } from "./session-state";

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

/** sharedWrapper builds a wrapper around an explicit, caller-held
 * QueryClient — useSignOut's tests need to observe useSession transition
 * after sign-out, which requires both hooks to share the same client (the
 * plain `wrapper` above builds a fresh, private one per renderHook call). */
function sharedWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

afterEach(() => {
  document.cookie = "beecon_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/";
  document.cookie = "beecon_session=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/";
});

describe("useSession", () => {
  it("resolves with the authenticated operator when GET /auth/me returns 200", async () => {
    server.use(http.get("/api/v1/auth/me", () => HttpResponse.json({ id: "op_1", email: "operator@example.com" })));

    const { result } = renderHook(() => useSession(), { wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({ id: "op_1", email: "operator@example.com" });
  });

  it("resolves to an error (no session) when GET /auth/me returns 401 — the root guard falls back to LoginScreen on this", async () => {
    server.use(http.get("/api/v1/auth/me", () => new HttpResponse(null, { status: 401 })));

    const { result } = renderHook(() => useSession(), { wrapper });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.data).toBeUndefined();
  });
});

// --- useSignOut (Phase 5 Slice 2, PD49 AC1/AC7): POSTs /auth/logout, then
// invalidates the auth.me probe either way — so the SPA falls back to
// LoginScreen even if the logout call itself fails. ---
describe("useSignOut", () => {
  it("POSTs to /api/v1/auth/logout", async () => {
    let logoutCalled = false;
    server.use(
      http.post("/api/v1/auth/logout", () => {
        logoutCalled = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { result } = renderHook(() => useSignOut(), { wrapper });

    result.current();

    await waitFor(() => expect(logoutCalled).toBe(true));
  });

  it("invalidates the auth.me probe after a successful logout, so useSession transitions to unauthenticated (LoginScreen)", async () => {
    let meCallCount = 0;
    server.use(
      http.get("/api/v1/auth/me", () => {
        meCallCount++;
        return meCallCount === 1
          ? HttpResponse.json({ id: "op_1", email: "operator@example.com" })
          : new HttpResponse(null, { status: 401 });
      }),
      http.post("/api/v1/auth/logout", () => new HttpResponse(null, { status: 204 })),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const sessionHook = renderHook(() => useSession(), { wrapper: sharedWrapper(queryClient) });
    await waitFor(() => expect(sessionHook.result.current.isSuccess).toBe(true));
    const signOutHook = renderHook(() => useSignOut(), { wrapper: sharedWrapper(queryClient) });

    signOutHook.result.current();

    await waitFor(() => expect(sessionHook.result.current.isError).toBe(true));
    expect(meCallCount).toBe(2);
  });

  it("still invalidates the auth.me probe even when the /auth/logout call itself fails (never leaves the operator stuck in the shell)", async () => {
    let meCallCount = 0;
    server.use(
      http.get("/api/v1/auth/me", () => {
        meCallCount++;
        return meCallCount === 1
          ? HttpResponse.json({ id: "op_1", email: "operator@example.com" })
          : new HttpResponse(null, { status: 401 });
      }),
      http.post("/api/v1/auth/logout", () => new HttpResponse(null, { status: 500 })),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const sessionHook = renderHook(() => useSession(), { wrapper: sharedWrapper(queryClient) });
    await waitFor(() => expect(sessionHook.result.current.isSuccess).toBe(true));
    const signOutHook = renderHook(() => useSignOut(), { wrapper: sharedWrapper(queryClient) });

    signOutHook.result.current();

    await waitFor(() => expect(sessionHook.result.current.isError).toBe(true));
    expect(meCallCount).toBe(2);
  });
});

describe("readCsrfToken", () => {
  it("returns null when the beecon_csrf cookie is absent", () => {
    expect(readCsrfToken()).toBeNull();
  });

  it("reads the beecon_csrf cookie's decoded value", () => {
    document.cookie = "beecon_csrf=abc%20123";

    expect(readCsrfToken()).toBe("abc 123");
  });

  it("does not confuse a differently-named cookie for beecon_csrf", () => {
    document.cookie = "beecon_session=some-session-token";

    expect(readCsrfToken()).toBeNull();
  });
});

/** singletonWrapper wraps with the app's real singleton queryClient
 * (lib/query.ts), not a fresh per-test instance: useReauthRequired reads
 * queryKeys.auth.reauthRequired() through React context (no explicit
 * `client` option on its own useQuery call), while markSessionExpiredMidWork
 * (lib/session-state.ts) always writes to that same singleton — the two only
 * observe each other when a test's provider wraps the singleton itself,
 * exactly like main.tsx does in production. */
function singletonWrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={singletonQueryClient}>{children}</QueryClientProvider>;
}

// --- useReauthRequired (Slice 5): reads the mid-work reauth flag
// markSessionExpiredMidWork sets — false until a mid-session 401 flags it. ---
describe("useReauthRequired", () => {
  afterEach(() => {
    singletonQueryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
  });

  it("is false before any 401 has flagged the session expired mid-work", () => {
    const { result } = renderHook(() => useReauthRequired(), { wrapper: singletonWrapper });

    expect(result.current).toBe(false);
  });

  it("becomes true once markSessionExpiredMidWork has flagged the session expired mid-work", async () => {
    const { result } = renderHook(() => useReauthRequired(), { wrapper: singletonWrapper });

    markSessionExpiredMidWork();

    await waitFor(() => expect(result.current).toBe(true));
  });
});

// --- cachedOperatorEmail (Slice 5): a plain read (not a hook) of whatever
// email the last successful /auth/me probe cached — ReauthModal's own
// "already know who you are, just ask for the password" prefill. ---
describe("cachedOperatorEmail", () => {
  afterEach(() => {
    singletonQueryClient.removeQueries({ queryKey: queryKeys.auth.me() });
  });

  it("is undefined when no session has ever been established", () => {
    expect(cachedOperatorEmail()).toBeUndefined();
  });

  it("returns the email cached by the last successful auth.me probe", () => {
    singletonQueryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });

    expect(cachedOperatorEmail()).toBe("operator@example.com");
  });
});
