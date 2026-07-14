import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";

import { server } from "./src/test/msw/server";

/**
 * MSW lifecycle (§2.9): listen before any test runs, reset per-test
 * overrides after each test so they never leak into the next, close once
 * the whole run finishes. `onUnhandledRequest: "error"` fails a test loudly
 * if a component fetches something no handler expects, rather than letting
 * the request silently hang.
 */
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());

/**
 * Testing Library's own auto-cleanup only registers itself when it detects
 * vitest's globals injected into the global scope; this project's
 * vitest.config.ts deliberately sets `globals: false` (explicit imports
 * everywhere else), so `cleanup()` is wired here instead — otherwise every
 * test after the first would render on top of the previous test's leftover
 * DOM.
 */
afterEach(() => cleanup());

/**
 * jsdom does not implement window.scrollTo; TanStack Router's scroll
 * restoration calls it on every navigation, which would otherwise print a
 * "Not implemented" error to the console on every router-driven test. This
 * is a test-environment stub only — real browsers implement scrollTo.
 */
window.scrollTo = () => {};

/**
 * jsdom does not implement window.matchMedia; ThemeToggle.tsx (part of the
 * shell every gate+shell test mounts) reads it to seed the initial light/
 * dark theme. Returns a static MediaQueryList-shaped stub reporting "no
 * dark-mode preference" — theme toggling itself is not under test here.
 */
window.matchMedia =
  window.matchMedia ??
  ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }));

/**
 * jsdom implements neither ResizeObserver nor real layout (every element's
 * getBoundingClientRect() is all zeros) — recharts' ResponsiveContainer
 * (the dashboard's charts, Slice 3) reads both to size itself, and without
 * them it measures a permanent 0×0 and never renders any chart content at
 * all. A minimal ResizeObserver stub (recharts only needs the class to
 * exist — it reads the container's own getBoundingClientRect() directly
 * rather than waiting on the observer callback) plus a fixed non-zero
 * fallback rect for any element jsdom would otherwise report as 0×0 is
 * enough for recharts to lay out real SVG content in tests.
 */
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
window.ResizeObserver = window.ResizeObserver ?? (ResizeObserverStub as unknown as typeof ResizeObserver);

const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect;
HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
  const rect = originalGetBoundingClientRect.call(this);
  if (rect.width === 0 && rect.height === 0) {
    return { ...rect, width: 600, height: 300, top: 0, left: 0, right: 600, bottom: 300 };
  }
  return rect;
};
