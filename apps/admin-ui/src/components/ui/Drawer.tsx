import * as Dialog from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";

export interface DrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}

/**
 * Drawer is the right-side detail panel the design brief specifies for
 * scan-heavy surfaces (DESIGN.md §0#4/§6: logs, events, connections) —
 * built on Radix Dialog anchored to the right edge instead of centered, so
 * its focus trap, Esc-to-close, and return-focus-to-trigger behavior
 * (DESIGN.md §9) come from the same primitive the rest of the console uses
 * rather than being hand-rolled. `drawer-overlay`/`drawer-content` (globals.css)
 * supply the slide-in motion, gated by prefers-reduced-motion at the
 * animation-duration level.
 */
export function Drawer({ open, onOpenChange, title, description, children, footer }: DrawerProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="drawer-overlay fixed inset-0 z-30 bg-black/40" />
        <Dialog.Content
          aria-describedby={description ? "drawer-description" : undefined}
          className="drawer-content fixed inset-y-0 right-0 z-40 flex w-full max-w-[560px] flex-col border-l border-border bg-surface shadow-lg focus:outline-none"
        >
          <div className="flex items-start justify-between gap-4 border-b border-border px-6 py-4">
            <div className="min-w-0">
              <Dialog.Title className="text-lg font-semibold text-text">{title}</Dialog.Title>
              {description ? (
                <div id="drawer-description" className="mt-1 text-sm text-text-secondary">
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
          <div className="flex-1 overflow-y-auto px-6 py-4">{children}</div>
          {footer ? <div className="flex items-center justify-end gap-2 border-t border-border px-6 py-4">{footer}</div> : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
