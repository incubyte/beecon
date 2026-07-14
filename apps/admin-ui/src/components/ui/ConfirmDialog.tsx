import * as AlertDialog from "@radix-ui/react-alert-dialog";
import type { ReactNode } from "react";

export interface ConfirmDialogProps {
  trigger: ReactNode;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  isConfirming?: boolean;
}

/** ConfirmDialog is the plain confirm/cancel modal DESIGN.md §7 specifies
 * for destructive actions (e.g. deleting a trigger instance, Slice 2 AC6) —
 * built on Radix AlertDialog so focus trap, Esc-to-close, and
 * return-focus-to-trigger (DESIGN.md §9) come from the primitive. Use
 * TypeToConfirm instead for the highest-risk destructive actions. */
export function ConfirmDialog({ trigger, title, description, confirmLabel, onConfirm, isConfirming }: ConfirmDialogProps) {
  return (
    <AlertDialog.Root>
      <AlertDialog.Trigger asChild>{trigger}</AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="fixed inset-0 z-30 bg-black/40" />
        <AlertDialog.Content className="fixed top-1/2 left-1/2 z-40 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-6 shadow-lg focus:outline-none">
          <AlertDialog.Title className="text-lg font-semibold text-text">{title}</AlertDialog.Title>
          <AlertDialog.Description className="mt-2 text-sm text-text-secondary">{description}</AlertDialog.Description>
          <div className="mt-6 flex justify-end gap-2">
            <AlertDialog.Cancel asChild>
              <button
                type="button"
                className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
              >
                Cancel
              </button>
            </AlertDialog.Cancel>
            <AlertDialog.Action asChild>
              <button
                type="button"
                onClick={onConfirm}
                disabled={isConfirming}
                className="min-h-11 rounded-md bg-error-solid px-4 text-sm font-medium text-white transition-colors hover:bg-error-solid/90 disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
              >
                {isConfirming ? "Working…" : confirmLabel}
              </button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
