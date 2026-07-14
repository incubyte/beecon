import { Check, Copy } from "lucide-react";
import { useState, type MouseEvent } from "react";

import { truncateId } from "@/lib/format";

export interface CopyIdChipProps {
  id: string;
}

/** CopyIdChip renders a long CUID2 id (org_..., conn_..., tool_...) mono,
 * truncated, with a click-to-copy affordance (DESIGN.md §6/§7). The full id
 * stays in the DOM (via `title`) for screen readers and browser search,
 * even though the visible label is shortened. */
export function CopyIdChip({ id }: CopyIdChipProps) {
  const [copied, setCopied] = useState(false);

  async function handleCopy(event: MouseEvent) {
    event.stopPropagation();
    try {
      await navigator.clipboard.writeText(id);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied by the browser; the chip simply
      // stays in its un-copied state rather than throwing.
    }
  }

  return (
    <button
      type="button"
      onClick={handleCopy}
      title={id}
      aria-label={copied ? `Copied ${id}` : `Copy id ${id}`}
      className="inline-flex min-h-11 items-center gap-1.5 rounded-md border border-border bg-surface-muted px-2.5 py-1.5 font-mono text-[13px] text-text-secondary transition-colors hover:border-border-strong hover:text-text cursor-pointer"
    >
      <span>{truncateId(id)}</span>
      {copied ? (
        <Check className="size-3.5 text-success-solid" aria-hidden="true" />
      ) : (
        <Copy className="size-3.5" aria-hidden="true" />
      )}
    </button>
  );
}
