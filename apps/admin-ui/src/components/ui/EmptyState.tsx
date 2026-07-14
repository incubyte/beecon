import type { ReactNode } from "react";

export interface EmptyStateProps {
  title: string;
  description?: string;
  action?: ReactNode;
}

/** EmptyState is the first-class "nothing here yet" surface every list
 * needs (DESIGN.md §7): headline + one-line description + an optional
 * primary action, light on illustration. */
export function EmptyState({ title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-border px-6 py-16 text-center">
      <p className="text-base font-medium text-text">{title}</p>
      {description ? <p className="max-w-[70ch] text-sm text-text-secondary">{description}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  );
}
