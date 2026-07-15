import { afterEach, describe, expect, it, vi } from "vitest";

import { queryClient, queryKeys } from "./query";

import { clearSessionExpiredMidWork, markSessionExpiredMidWork, resolveSessionExpiredMidWork } from "./session-state";

/**
 * session-state.ts holds the "session expired mid-work" flag as react-query
 * cache data under queryKeys.auth.reauthRequired() (Phase 5 Slice 5) — these
 * tests exercise its three exported functions directly, against the real
 * singleton queryClient (the same one lib/auth.ts's useReauthRequired and
 * ReauthModal ultimately read through), resetting the flag afterward so it
 * never leaks into another test file sharing this module.
 */

afterEach(() => {
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
});

describe("markSessionExpiredMidWork", () => {
  it("sets the reauthRequired cache entry to true", () => {
    markSessionExpiredMidWork();

    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(true);
  });
});

describe("clearSessionExpiredMidWork", () => {
  it("sets the reauthRequired cache entry back to false", () => {
    markSessionExpiredMidWork();

    clearSessionExpiredMidWork();

    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false);
  });
});

describe("resolveSessionExpiredMidWork", () => {
  it("clears the reauthRequired flag", () => {
    markSessionExpiredMidWork();

    resolveSessionExpiredMidWork();

    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false);
  });

  it("refetches every currently active query, so the page resumes with fresh post-reauth data", () => {
    const refetchSpy = vi.spyOn(queryClient, "refetchQueries");
    markSessionExpiredMidWork();

    resolveSessionExpiredMidWork();

    expect(refetchSpy).toHaveBeenCalledWith({ type: "active" });
    refetchSpy.mockRestore();
  });
});
