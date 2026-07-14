import { X } from "lucide-react";

export interface FilterChipProps {
  label: string;
  onRemove: () => void;
}

/** FilterChip is one individually-removable applied-filter pill (DESIGN.md
 * §6/§7, Slice 2 AC3): the filter's own control (a select, a search input)
 * stays the source of truth — removing the chip clears that control's
 * value, it does not carry independent state of its own. */
export function FilterChip({ label, onRemove }: FilterChipProps) {
  return (
    <span className="inline-flex min-h-11 items-center gap-1 rounded-pill border border-border-strong bg-surface-muted py-1 pr-1 pl-3 text-xs font-medium text-text">
      {label}
      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove filter: ${label}`}
        className="flex min-h-11 min-w-11 items-center justify-center rounded-full text-text-muted transition-colors hover:bg-border hover:text-text cursor-pointer"
      >
        <X className="size-3.5" aria-hidden="true" />
      </button>
    </span>
  );
}
