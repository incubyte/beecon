import * as AlertDialog from "@radix-ui/react-alert-dialog";
import { useId, useState, type ReactNode } from "react";

export interface TypeToConfirmProps {
  trigger: ReactNode;
  title: string;
  description: string;
  /** The exact text the operator must type before the destructive action
   * enables — typically the entity's own id, so the operator is looking
   * straight at what they're about to delete (DESIGN.md §7's "key-shown-once"
   * ceremony applied to the highest-risk deletes). */
  confirmText: string;
  confirmLabel: string;
  onConfirm: () => void;
  isConfirming?: boolean;
}

/** TypeToConfirm is DESIGN.md §7's highest-risk destructive confirmation:
 * the primary action stays disabled until the operator types confirmText
 * exactly. Built on Radix AlertDialog (focus trap, Esc, return-focus —
 * DESIGN.md §9) with the typed value reset whenever the dialog re-opens. */
export function TypeToConfirm({ trigger, title, description, confirmText, confirmLabel, onConfirm, isConfirming }: TypeToConfirmProps) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const inputId = useId();
  const matches = typed === confirmText;

  function handleOpenChange(nextOpen: boolean) {
    setOpen(nextOpen);
    setTyped("");
  }

  function handleConfirm() {
    if (!matches) {
      return;
    }
    onConfirm();
    setOpen(false);
  }

  return (
    <AlertDialog.Root open={open} onOpenChange={handleOpenChange}>
      <AlertDialog.Trigger asChild>{trigger}</AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 z-30 bg-black/40" />
        <AlertDialog.Content className="fixed top-1/2 left-1/2 z-40 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-6 shadow-lg focus:outline-none">
          <AlertDialog.Title className="text-lg font-semibold text-text">{title}</AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-sm text-text-secondary">{description}</AlertDialog.Description>

          <label htmlFor={inputId} className="mt-4 block text-sm text-text-secondary">
            Type <span className="font-mono text-text">{confirmText}</span> to confirm
          </label>
          <input
            id={inputId}
            type="text"
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
            autoComplete="off"
            spellCheck={false}
            className="mt-1.5 min-h-11 w-full rounded-md border border-border-strong bg-surface px-3 font-mono text-sm text-text focus-visible:border-primary"
          />

          <div className="mt-6 flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <button
                type="button"
                className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
              >
                Cancel
              </button>
            </AlertDialog.Cancel>
            <button
              type="button"
              onClick={handleConfirm}
              disabled={!matches || isConfirming}
              className="min-h-11 rounded-md bg-error-solid px-4 text-sm font-medium text-white transition-colors hover:bg-error-solid/90 disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {isConfirming ? "Working…" : confirmLabel}
            </button>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
