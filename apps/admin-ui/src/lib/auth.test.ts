import { afterEach, describe, expect, it, vi } from "vitest";

import { clearAdminKey, getAdminKey, setAdminKey } from "./auth";

afterEach(() => {
  clearAdminKey();
});

describe("the in-memory admin-key store (PD39)", () => {
  it("starts with no key", () => {
    expect(getAdminKey()).toBeNull();
  });

  it("returns the key that was set", () => {
    setAdminKey("beecon_admin_test-key");

    expect(getAdminKey()).toBe("beecon_admin_test-key");
  });

  it("forgets the key once cleared", () => {
    setAdminKey("beecon_admin_test-key");

    clearAdminKey();

    expect(getAdminKey()).toBeNull();
  });

  // --- Security-critical (PD39): the admin key must never touch persistent
  // browser storage. A single accidental `localStorage.setItem(..., key)`
  // here would silently widen the credential's exposure window past what
  // PD39 accepted (the README's own explicit warning) — this test is the
  // adversarial guard against that regression, mirroring Phase 2's own
  // key-safety test style. ---

  it("never writes the key to localStorage", () => {
    const setItemSpy = vi.spyOn(Storage.prototype, "setItem");

    setAdminKey("beecon_admin_never-persist-me");

    for (const call of setItemSpy.mock.calls) {
      expect(call).not.toContain("beecon_admin_never-persist-me");
    }
    setItemSpy.mockRestore();
  });

  it("never writes the key to sessionStorage", () => {
    const localSetItem = vi.spyOn(window.localStorage, "setItem");
    const sessionSetItem = vi.spyOn(window.sessionStorage, "setItem");

    setAdminKey("beecon_admin_never-persist-me");

    expect(localSetItem).not.toHaveBeenCalledWith(expect.anything(), expect.stringContaining("beecon_admin_never-persist-me"));
    expect(sessionSetItem).not.toHaveBeenCalledWith(expect.anything(), expect.stringContaining("beecon_admin_never-persist-me"));
    localSetItem.mockRestore();
    sessionSetItem.mockRestore();
  });

  it("never places the key under any localStorage key afterward", () => {
    setAdminKey("beecon_admin_never-persist-me");

    for (let i = 0; i < window.localStorage.length; i++) {
      const key = window.localStorage.key(i);
      const value = key ? window.localStorage.getItem(key) : null;
      expect(value).not.toBe("beecon_admin_never-persist-me");
    }
  });

  // --- Reload semantics (AC3/AC4): reloading the tab or opening a new tab
  // starts a fresh JS module instance, so the key store must start at null
  // again rather than somehow surviving. vi.resetModules() + a fresh
  // dynamic import is the closest a single test process gets to actually
  // reloading the page: it forces a brand-new module instance, exactly the
  // scenario PD39 relies on. ---

  it("a fresh module instance (simulating a reload or a new tab) starts with no key, independent of any previous instance", async () => {
    setAdminKey("beecon_admin_before-reload");
    expect(getAdminKey()).toBe("beecon_admin_before-reload");

    vi.resetModules();
    const freshAuthModule = await import("./auth");

    expect(freshAuthModule.getAdminKey()).toBeNull();
  });
});
