import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";

export interface ModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}

/**
 * Modal is the plain centered dialog DESIGN.md §7's component inventory
 * names for form-heavy actions (creating an end-user, creating an API key)
 * — distinct from Drawer (right-anchored, scan-heavy detail) and from
 * SecretOnceModal (checkbox-gated, cannot be dismissed early). Built on
 * Radix Dialog so focus trap, Esc-to-close, and return-focus-to-trigger
 * (DESIGN.md §9) come from the primitive, freely dismissable like every
 * other non-destructive, non-secret dialog.
 */
export function Modal({ open, onOpenChange, title, description, children, footer }: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-30 bg-black/40" />
        <Dialog.Content
          aria-describedby={description ? "modal-description" : undefined}
          className="fixed top-1/2 left-1/2 z-40 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface shadow-lg focus:outline-none"
        >
          <div className="flex items-start justify-between gap-4 border-b border-border px-6 py-4">
            <div className="min-w-0">
              <Dialog.Title className="text-lg font-semibold text-text">{title}</Dialog.Title>
              {description ? (
                <div id="modal-description" className="mt-1 text-sm text-text-secondary">
                  {description}
                </div>
              ) : null}
            </div>
            <Dialog.Close asChild>
              <button
                type="button"
                aria-label="Close"
                className="flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
              >
                <X className="size-4" aria-hidden="true" />
              </button>
            </Dialog.Close>
          </div>
          <div className="px-6 py-4">{children}</div>
          {footer ? <div className="flex items-center justify-end gap-2 border-t border-border px-6 py-4">{footer}</div> : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
