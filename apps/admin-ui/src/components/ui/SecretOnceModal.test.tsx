import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SecretOnceModal, type SecretOnceModalProps } from "./SecretOnceModal";

/**
 * SecretOnceModal is DESIGN.md §7's credential-handling ceremony — the
 * highest-risk component in the console (Slice 4). These tests are
 * deliberately adversarial: the checkbox must genuinely gate dismissal (not
 * just visually), the secret must never touch persistent browser storage,
 * and once dismissed the secret must be gone from the DOM, not merely
 * hidden.
 */

const SECRET = "beecon_sk_the-full-secret-value-shown-exactly-once";

function renderModal(overrides: Partial<SecretOnceModalProps> = {}) {
  const onDismiss = vi.fn();
  const utils = render(
    <SecretOnceModal open={true} onDismiss={onDismiss} title="New API key" secret={SECRET} {...overrides} />,
  );
  return { onDismiss, ...utils };
}

beforeEach(() => {
  // jsdom implements neither Clipboard nor URL.createObjectURL by default;
  // these tests need to observe both calls, not merely tolerate their
  // absence (which SecretOnceModal's own try/catch already does for a real
  // browser that denies clipboard access).
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  window.URL.createObjectURL = vi.fn().mockReturnValue("blob:mock-url");
  window.URL.revokeObjectURL = vi.fn();
  // jsdom attempts a real page navigation when an <a download> element is
  // clicked (it doesn't honor the download attribute), logging a noisy
  // "Not implemented: navigation" error — stubbing click() keeps the
  // download flow's own side effects (createObjectURL/revokeObjectURL)
  // observable without jsdom trying to actually navigate.
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("SecretOnceModal", () => {
  it("shows the full secret in plaintext while open", () => {
    renderModal();

    expect(screen.getByText(SECRET)).toBeInTheDocument();
  });

  it("shows the 'you will not see this again' warning", () => {
    renderModal();

    expect(screen.getByText(/will not be able to see this secret again/i)).toBeInTheDocument();
  });

  it("renders the copy and download affordances", () => {
    renderModal();

    expect(screen.getByRole("button", { name: /copy secret/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /download secret/i })).toBeInTheDocument();
  });

  // --- Checkbox-gated dismissal (the core credential-handling contract). ---

  it("disables the Done button until the 'I've stored it safely' checkbox is checked", () => {
    renderModal();

    expect(screen.getByRole("button", { name: /done/i })).toBeDisabled();
  });

  it("enables the Done button once the checkbox is checked", () => {
    renderModal();

    fireEvent.click(screen.getByRole("checkbox"));

    expect(screen.getByRole("button", { name: /done/i })).toBeEnabled();
  });

  it("calls onDismiss only after the checkbox is checked and Done is clicked", () => {
    const { onDismiss } = renderModal();

    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /done/i }));

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("clicking the disabled Done button before checking the box never calls onDismiss", () => {
    const { onDismiss } = renderModal();

    fireEvent.click(screen.getByRole("button", { name: /done/i }));

    expect(onDismiss).not.toHaveBeenCalled();
  });

  // --- Adversarial: Esc / overlay dismissal must not bypass the gate. ---

  it("pressing Escape while unacknowledged does not dismiss the modal", () => {
    const { onDismiss } = renderModal();

    fireEvent.keyDown(screen.getByText(SECRET), { key: "Escape", code: "Escape" });

    expect(onDismiss).not.toHaveBeenCalled();
    expect(screen.getByText(SECRET)).toBeInTheDocument();
  });

  // --- Copy / download actually invoke the right browser API with the
  // secret, not just render a button that looks functional. ---

  it("clicking Copy writes the exact secret to the clipboard", async () => {
    renderModal();

    fireEvent.click(screen.getByRole("button", { name: /copy secret/i }));

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(SECRET);
  });

  it("clicking Download creates an object URL from a text/plain blob sized exactly to the secret", () => {
    renderModal();

    fireEvent.click(screen.getByRole("button", { name: /download secret/i }));

    expect(window.URL.createObjectURL).toHaveBeenCalledTimes(1);
    const blob = (window.URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls[0][0] as Blob;
    expect(blob.type).toBe("text/plain");
    // jsdom's Blob has no .text()/.arrayBuffer() implementation; comparing
    // byte size (the secret is plain ASCII, so size === length) is enough to
    // confirm the blob wraps the secret itself, not an empty/placeholder
    // value, without depending on a Blob-reading API jsdom doesn't provide.
    expect(blob.size).toBe(SECRET.length);
  });

  // --- Once dismissed, the secret must be gone from the DOM — not just
  // toggled invisible — and must never be re-shown without a fresh
  // Issue/Rotate response. ---

  it("the secret is removed from the DOM once the modal is closed", () => {
    const { rerender } = renderModal();
    expect(screen.getByText(SECRET)).toBeInTheDocument();

    rerender(<SecretOnceModal open={false} onDismiss={vi.fn()} title="New API key" secret={SECRET} />);

    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
  });

  it("re-opening with an empty secret (the caller's post-dismiss state) never re-shows the old secret", () => {
    const { rerender } = renderModal();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /done/i }));

    // Mirrors ApiKeysPage's own post-dismiss state: revealedSecret is cleared
    // to null, so open flips to false and the secret prop becomes "".
    rerender(<SecretOnceModal open={false} onDismiss={vi.fn()} title="New API key" secret="" />);

    expect(screen.queryByText(SECRET)).not.toBeInTheDocument();
  });

  // --- Adversarial (mirrors auth.test.ts's own admin-key-never-persisted
  // guard): the secret must never touch localStorage/sessionStorage, however
  // it's interacted with. ---

  it("never writes the secret to localStorage or sessionStorage while open and interacted with", () => {
    const localSetItem = vi.spyOn(window.localStorage, "setItem");
    const sessionSetItem = vi.spyOn(window.sessionStorage, "setItem");

    renderModal();
    fireEvent.click(screen.getByRole("button", { name: /copy secret/i }));
    fireEvent.click(screen.getByRole("button", { name: /download secret/i }));
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByRole("button", { name: /done/i }));

    expect(localSetItem).not.toHaveBeenCalledWith(expect.anything(), expect.stringContaining(SECRET));
    expect(sessionSetItem).not.toHaveBeenCalledWith(expect.anything(), expect.stringContaining(SECRET));
  });
});
