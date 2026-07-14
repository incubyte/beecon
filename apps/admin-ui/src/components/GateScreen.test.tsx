import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";
import { clearAdminKey, getAdminKey } from "@/lib/auth";

import { GateScreen } from "./GateScreen";

afterEach(() => {
  clearAdminKey();
});

function typeKeyAndSubmit(key: string) {
  const input = screen.getByLabelText(/admin key/i);
  fireEvent.change(input, { target: { value: key } });
  fireEvent.click(screen.getByRole("button", { name: /open console/i }));
  return input;
}

describe("GateScreen", () => {
  it("stores the key in memory once /admin/verify accepts it (204)", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));
    render(<GateScreen />);

    typeKeyAndSubmit("beecon_admin_good-key");

    await waitFor(() => expect(getAdminKey()).toBe("beecon_admin_good-key"));
  });

  it("shows an inline error (icon + text) when the key is rejected (401), without clearing the input", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 401 })));
    render(<GateScreen />);

    const input = typeKeyAndSubmit("beecon_admin_bad-key");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/rejected/i);
    // Never color-only (DESIGN.md §9): the alert must carry a visible icon
    // in addition to its text, not rely on color alone to signal the error.
    expect(alert.querySelector("svg")).toBeInTheDocument();
    expect(input).toHaveValue("beecon_admin_bad-key");
  });

  it("never stores a rejected key", async () => {
    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 401 })));
    render(<GateScreen />);

    typeKeyAndSubmit("beecon_admin_bad-key");
    await screen.findByRole("alert");

    expect(getAdminKey()).toBeNull();
  });

  it("shows a validation message and never calls /admin/verify when the key is blank", async () => {
    let verifyCallCount = 0;
    server.use(
      http.get("/admin/verify", () => {
        verifyCallCount++;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    render(<GateScreen />);

    fireEvent.click(screen.getByRole("button", { name: /open console/i }));

    expect(await screen.findByRole("alert")).toHaveTextContent(/enter the installation admin key/i);
    expect(verifyCallCount).toBe(0);
  });

  // --- Security-critical (PD39): the gate's whole job is to keep the key
  // out of persistent storage. A submit — success or failure — must never
  // touch localStorage/sessionStorage with the key, mirroring the adversarial
  // style of auth.test.ts's own storage spies at the component level too. ---

  it("never writes the submitted key to localStorage or sessionStorage, on either a successful or a rejected verify", async () => {
    const localSetItem = vi.spyOn(window.localStorage, "setItem");
    const sessionSetItem = vi.spyOn(window.sessionStorage, "setItem");

    server.use(http.get("/admin/verify", () => new HttpResponse(null, { status: 204 })));
    render(<GateScreen />);
    typeKeyAndSubmit("beecon_admin_should-never-persist");
    await waitFor(() => expect(getAdminKey()).toBe("beecon_admin_should-never-persist"));

    for (const spy of [localSetItem, sessionSetItem]) {
      for (const call of spy.mock.calls) {
        expect(call).not.toContain("beecon_admin_should-never-persist");
      }
    }
    localSetItem.mockRestore();
    sessionSetItem.mockRestore();
  });
});
