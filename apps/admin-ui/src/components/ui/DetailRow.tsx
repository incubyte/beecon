import type { ReactNode } from "react";

export interface DetailRowProps {
  label: string;
  children: ReactNode;
}

/** DetailRow is one labeled row in a detail drawer's definition list
 * (DESIGN.md §7's "key/value definition lists") — shared by the Slice 3
 * drawers (Logs, Events & Delivery) now that a third detail surface needs
 * the same label-over-value shape ConnectionDrawer (Slice 2) first
 * introduced inline. */
export function DetailRow({ label, children }: DetailRowProps) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs font-medium tracking-wide text-text-muted uppercase">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}
