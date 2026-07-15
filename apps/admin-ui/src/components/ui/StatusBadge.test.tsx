import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge } from "./StatusBadge";

/** StatusBadge.test.tsx focuses on the integrationVisibility taxonomy Slice
 * 5 adds (VISIBLE/HIDDEN/NOT_ALLOWED) — the governance editor's own
 * per-integration effective-visibility pill (GovernancePage.test.tsx exercises
 * it in situ; these tests pin the taxonomy mapping itself in isolation). Every
 * assertion checks BOTH the visible text label and an icon rendering
 * (aria-hidden, decorative) — DESIGN.md's "color is never the only signal"
 * mandate applies to every taxonomy, this one included. */
describe("StatusBadge integrationVisibility taxonomy", () => {
  it("VISIBLE renders the label 'Visible' with an icon, not color alone", () => {
    render(<StatusBadge taxonomy="integrationVisibility" status="VISIBLE" />);

    expect(screen.getByText("Visible")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("HIDDEN renders the label 'Hidden' with an icon", () => {
    render(<StatusBadge taxonomy="integrationVisibility" status="HIDDEN" />);

    expect(screen.getByText("Hidden")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("NOT_ALLOWED renders the human label 'Not allowed' with an icon", () => {
    render(<StatusBadge taxonomy="integrationVisibility" status="NOT_ALLOWED" />);

    expect(screen.getByText("Not allowed")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("VISIBLE, HIDDEN, and NOT_ALLOWED each render visually distinct text colors, never relying on background alone", () => {
    const { unmount: unmountVisible } = render(<StatusBadge taxonomy="integrationVisibility" status="VISIBLE" />);
    const visibleClass = screen.getByText("Visible").className;
    unmountVisible();

    const { unmount: unmountHidden } = render(<StatusBadge taxonomy="integrationVisibility" status="HIDDEN" />);
    const hiddenClass = screen.getByText("Hidden").className;
    unmountHidden();

    render(<StatusBadge taxonomy="integrationVisibility" status="NOT_ALLOWED" />);
    const notAllowedClass = screen.getByText("Not allowed").className;

    expect(new Set([visibleClass, hiddenClass, notAllowedClass]).size).toBe(3);
  });

  it("an unrecognized visibility value falls back to a neutral pill carrying the raw value as its label, rather than crashing", () => {
    render(<StatusBadge taxonomy="integrationVisibility" status="SOMETHING_NEW" />);

    expect(screen.getByText("SOMETHING_NEW")).toBeInTheDocument();
  });
});

/** StatusBadge endpoint taxonomy (Slice 8, PD45): ENABLED/DISABLED/
 * DISABLED_AUTO — DISABLED_AUTO is the auto-disable bookkeeping's own
 * outcome and must read as visually distinct (not just a same-tinted
 * "disabled" restated) from an operator's own DISABLED, since an operator
 * scanning the webhook-endpoints list needs to tell "I turned this off" from
 * "the platform quarantined this after repeated failures" at a glance. */
describe("StatusBadge endpoint taxonomy", () => {
  it("ENABLED renders the label 'Enabled' with an icon, not color alone", () => {
    render(<StatusBadge taxonomy="endpoint" status="ENABLED" />);

    expect(screen.getByText("Enabled")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("DISABLED renders the label 'Disabled' with an icon", () => {
    render(<StatusBadge taxonomy="endpoint" status="DISABLED" />);

    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("DISABLED_AUTO renders the human label 'Auto-disabled' with an icon", () => {
    render(<StatusBadge taxonomy="endpoint" status="DISABLED_AUTO" />);

    expect(screen.getByText("Auto-disabled")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("an operator-DISABLED endpoint and a platform-DISABLED_AUTO endpoint render visually distinct text colors, not the same 'disabled' look", () => {
    const { unmount: unmountDisabled } = render(<StatusBadge taxonomy="endpoint" status="DISABLED" />);
    const disabledClass = screen.getByText("Disabled").className;
    unmountDisabled();

    render(<StatusBadge taxonomy="endpoint" status="DISABLED_AUTO" />);
    const disabledAutoClass = screen.getByText("Auto-disabled").className;

    expect(disabledAutoClass).not.toBe(disabledClass);
  });

  it("ENABLED, DISABLED, and DISABLED_AUTO each render visually distinct text colors, never relying on background alone", () => {
    const { unmount: unmountEnabled } = render(<StatusBadge taxonomy="endpoint" status="ENABLED" />);
    const enabledClass = screen.getByText("Enabled").className;
    unmountEnabled();

    const { unmount: unmountDisabled } = render(<StatusBadge taxonomy="endpoint" status="DISABLED" />);
    const disabledClass = screen.getByText("Disabled").className;
    unmountDisabled();

    render(<StatusBadge taxonomy="endpoint" status="DISABLED_AUTO" />);
    const disabledAutoClass = screen.getByText("Auto-disabled").className;

    expect(new Set([enabledClass, disabledClass, disabledAutoClass]).size).toBe(3);
  });
});

/** StatusBadge operator taxonomy (Phase 5 Slice 4): ACTIVE/DISABLED — the
 * operators list's own status column (OperatorsPage.test.tsx exercises it in
 * situ; these tests pin the taxonomy mapping itself in isolation, the same
 * precedent every other taxonomy block above already sets). */
describe("StatusBadge operator taxonomy", () => {
  it("ACTIVE renders the label 'Active' with an icon, not color alone", () => {
    render(<StatusBadge taxonomy="operator" status="ACTIVE" />);

    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("DISABLED renders the label 'Disabled' with an icon", () => {
    render(<StatusBadge taxonomy="operator" status="DISABLED" />);

    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(document.querySelector("svg[aria-hidden='true']")).not.toBeNull();
  });

  it("ACTIVE and DISABLED render visually distinct text colors, never relying on background alone", () => {
    const { unmount: unmountActive } = render(<StatusBadge taxonomy="operator" status="ACTIVE" />);
    const activeClass = screen.getByText("Active").className;
    unmountActive();

    render(<StatusBadge taxonomy="operator" status="DISABLED" />);
    const disabledClass = screen.getByText("Disabled").className;

    expect(activeClass).not.toBe(disabledClass);
  });
});
