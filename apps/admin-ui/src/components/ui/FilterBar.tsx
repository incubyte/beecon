import type { ReactNode } from "react";

export interface FilterBarProps {
  children: ReactNode;
}

/** FilterBar is the prominent bar pinned above a data table (DESIGN.md §6/§7):
 * facet controls plus their removable applied-filter chips live inside it. */
export function FilterBar({ children }: FilterBarProps) {
  return (
    <div role="search" className="flex flex-wrap items-center gap-3 rounded-lg border border-border bg-surface p-3">
      {children}
    </div>
  );
}
