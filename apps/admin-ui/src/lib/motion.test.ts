import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { usePrefersReducedMotion } from "./motion";

type Listener = (event: MediaQueryListEvent) => void;

/** stubMatchMedia replaces window.matchMedia with a controllable fake that
 * tracks its own change listener, so a test can flip `matches` after the
 * hook has already mounted and simulate the browser firing a real "change"
 * event (the user toggling their OS-level reduced-motion setting mid
 * session). */
function stubMatchMedia(initialMatches: boolean): { setMatches: (matches: boolean) => void } {
  let matches = initialMatches;
  let listener: Listener | null = null;
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    get matches() {
      return matches;
    },
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: (_event: string, cb: Listener) => {
      listener = cb;
    },
    removeEventListener: () => {
      listener = null;
    },
    dispatchEvent: vi.fn(),
  }));
  return {
    setMatches: (next: boolean) => {
      matches = next;
      listener?.({ matches: next } as MediaQueryListEvent);
    },
  };
}

describe("usePrefersReducedMotion", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns false when the OS reports no reduced-motion preference", () => {
    stubMatchMedia(false);

    const { result } = renderHook(() => usePrefersReducedMotion());

    expect(result.current).toBe(false);
  });

  it("returns true when the OS already prefers reduced motion at mount", () => {
    stubMatchMedia(true);

    const { result } = renderHook(() => usePrefersReducedMotion());

    expect(result.current).toBe(true);
  });

  it("updates when the media query's own change event fires mid-session", () => {
    const media = stubMatchMedia(false);
    const { result } = renderHook(() => usePrefersReducedMotion());
    expect(result.current).toBe(false);

    act(() => {
      media.setMatches(true);
    });

    expect(result.current).toBe(true);
  });
});
