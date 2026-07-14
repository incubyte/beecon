import type { ConfigChange } from "@/lib/api-types";

export interface ConfigChangeListProps {
  title: string;
  changes: ConfigChange[];
  emptyDescription: string;
}

/**
 * ConfigChangeList renders one array of ConfigChange lines — either a
 * dry-run's plan or an apply's applied list (Slice 9): each line names the
 * area (governance/retention/endpoint), the action, and a one-line
 * human-readable detail — the diff/plan preview the design brief asks the
 * import flow to show before anything is written.
 */
export function ConfigChangeList({ title, changes, emptyDescription }: ConfigChangeListProps) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-sm font-semibold text-text">{title}</h3>
      {changes.length === 0 ? (
        <p className="text-sm text-text-secondary">{emptyDescription}</p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {changes.map((change, index) => (
            <li
              key={`${change.area}-${change.field}-${index}`}
              className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm text-text"
            >
              <span className="inline-flex items-center rounded-pill bg-neutral-bg px-2 py-0.5 text-xs font-medium text-neutral-text uppercase">
                {change.area}
              </span>
              <span className="font-mono text-xs text-text-secondary">{change.action}</span>
              <span>{change.detail}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
