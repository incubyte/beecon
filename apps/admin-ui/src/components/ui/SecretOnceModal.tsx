import * as Dialog from "@radix-ui/react-dialog";
import { AlertTriangle, Check, Copy, Download } from "lucide-react";
import { useEffect, useId, useState, type ReactNode } from "react";

export interface SecretOnceModalProps {
  open: boolean;
  /** Called only once the operator has checked the "I've stored it safely"
   * box and explicitly dismissed the modal — never from an Esc press or an
   * overlay click while unacknowledged (see handleOpenChange below). */
  onDismiss: () => void;
  title: string;
  /** The full secret value, shown in plaintext exactly once. Never logged,
   * never re-fetchable — the caller must have just received it from an
   * Issue/Rotate response. */
  secret: string;
  helpText?: ReactNode;
  /** Base name for the downloaded file (without extension); defaults to
   * "beecon-secret". */
  fileNamePrefix?: string;
}

/**
 * SecretOnceModal is DESIGN.md §7's credential-handling ceremony: an API
 * key, webhook signing secret, or user token's raw value is shown in mono
 * text exactly once, with copy and download affordances, an explicit "you
 * will not see this again" warning (icon + text, never color-only), and a
 * checkbox that gates dismissal — the operator cannot close the modal
 * (Esc, overlay click, or the close control) until they confirm they've
 * stored the secret. Built on Radix Dialog for focus trap and keyboard
 * handling (DESIGN.md §9), but deliberately overrides its own close
 * triggers: Dialog stays fully controlled by `open`, and
 * onEscapeKeyDown/onPointerDownOutside are suppressed while unacknowledged
 * so the dialog visibly refuses to close rather than flickering shut.
 */
export function SecretOnceModal({ open, onDismiss, title, secret, helpText, fileNamePrefix }: SecretOnceModalProps) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [copied, setCopied] = useState(false);
  const checkboxId = useId();

  useEffect(() => {
    if (open) {
      setAcknowledged(false);
      setCopied(false);
    }
  }, [open, secret]);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied by the browser — the button simply
      // stays in its un-copied state rather than throwing.
    }
  }

  function handleDownload() {
    const blob = new Blob([secret], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${fileNamePrefix ?? "beecon-secret"}.txt`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  function handleOpenChange(next: boolean) {
    if (!next && !acknowledged) {
      // Ignore Radix's own attempt to close (Esc / overlay click) until the
      // operator has acknowledged — `open` stays externally controlled, so
      // the dialog remains mounted and visibly open.
      return;
    }
    if (!next) {
      onDismiss();
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-30 bg-black/40" />
        <Dialog.Content
          onEscapeKeyDown={(event) => {
            if (!acknowledged) {
              event.preventDefault();
            }
          }}
          onPointerDownOutside={(event) => {
            if (!acknowledged) {
              event.preventDefault();
            }
          }}
          onInteractOutside={(event) => {
            if (!acknowledged) {
              event.preventDefault();
            }
          }}
          aria-describedby="secret-once-warning"
          className="fixed top-1/2 left-1/2 z-40 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-6 shadow-lg focus:outline-none"
        >
          <Dialog.Title className="text-lg font-semibold text-text">{title}</Dialog.Title>
          {helpText ? <div className="mt-1 text-sm text-text-secondary">{helpText}</div> : null}

          <div className="mt-4 flex items-center gap-2 rounded-md border border-border-strong bg-surface-muted px-3 py-2.5">
            <code className="flex-1 overflow-x-auto font-mono text-sm break-all text-text">{secret}</code>
            <button
              type="button"
              onClick={handleCopy}
              aria-label={copied ? "Copied" : "Copy secret"}
              className="flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface hover:text-text cursor-pointer"
            >
              {copied ? (
                <Check className="size-4 text-success-solid" aria-hidden="true" />
              ) : (
                <Copy className="size-4" aria-hidden="true" />
              )}
            </button>
            <button
              type="button"
              onClick={handleDownload}
              aria-label="Download secret"
              className="flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface hover:text-text cursor-pointer"
            >
              <Download className="size-4" aria-hidden="true" />
            </button>
          </div>

          <div
            id="secret-once-warning"
            className="mt-4 flex items-start gap-2 rounded-md bg-warning-bg px-3 py-2.5 text-sm text-warning-text"
          >
            <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            <span>You will not be able to see this secret again. Store it somewhere safe now.</span>
          </div>

          <label htmlFor={checkboxId} className="mt-4 flex items-start gap-2.5 text-sm text-text">
            <input
              id={checkboxId}
              type="checkbox"
              checked={acknowledged}
              onChange={(event) => setAcknowledged(event.target.checked)}
              className="mt-0.5 size-4 shrink-0 cursor-pointer rounded border-border-strong"
            />
            <span>I&rsquo;ve stored this secret safely and understand it won&rsquo;t be shown again.</span>
          </label>

          <div className="mt-6 flex justify-end">
            <button
              type="button"
              onClick={onDismiss}
              disabled={!acknowledged}
              className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              Done
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
