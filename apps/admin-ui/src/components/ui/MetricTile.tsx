import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

export interface MetricTileProps {
  label: string;
  value: ReactNode;
  description?: string;
  icon?: LucideIcon;
}

/** MetricTile is the dashboard's headline-figure card (DESIGN.md §7): a
 * number, a short label, and an optional one-line description — the row of
 * these is the operability dashboard's first thing an operator scans
 * (Slice 3, AC4). */
export function MetricTile({ label, value, description, icon: Icon }: MetricTileProps) {
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface p-4">
      <div className="flex items-center gap-2 text-text-muted">
        {Icon ? <Icon className="size-4 shrink-0" aria-hidden="true" /> : null}
        <p className="text-xs font-medium tracking-wide uppercase">{label}</p>
      </div>
      <p className="text-2xl font-semibold text-text">{value}</p>
      {description ? <p className="text-xs text-text-secondary">{description}</p> : null}
    </div>
  );
}
