import * as Collapsible from "@radix-ui/react-collapsible";
import { Check, ChevronDown, ChevronRight, Copy } from "lucide-react";
import { useMemo, useState, type MouseEvent } from "react";

export interface CodeViewerProps {
  label: string;
  value: string;
  defaultOpen?: boolean;
}

/** RedactedPlaceholder mirrors server/internal/logging/redact.go's
 * `RedactedPlaceholder` constant (Slice 3, AC1/AC6): logging.Redact already
 * replaces every sensitive field's value with this exact string before an
 * entry is ever persisted, so the viewer never redacts anything itself — it
 * only has to make the marker it receives unmissable. */
const REDACTED_PLACEHOLDER = "[REDACTED]";

/**
 * CodeViewer is the mono, collapsible JSON payload viewer DESIGN.md §7
 * specifies for redacted log bodies (Slice 3, AC1): a pretty-printed,
 * line-by-line render so a line carrying the textual `[REDACTED]` marker can
 * be flagged with a tint AND bolded text — the tint is never the only
 * signal, since the marker's own text already says what happened (DESIGN.md
 * §9's "redaction is textual"). Falls back to the raw string when value
 * isn't valid JSON (e.g. a plain error message) rather than throwing.
 */
export function CodeViewer({ label, value, defaultOpen = true }: CodeViewerProps) {
  const [open, setOpen] = useState(defaultOpen);
  const [copied, setCopied] = useState(false);
  const formatted = useMemo(() => prettyPrintJson(value), [value]);
  const lines = useMemo(() => formatted.split("\n"), [formatted]);

  async function handleCopy(event: MouseEvent) {
    event.stopPropagation();
    try {
      await navigator.clipboard.writeText(formatted);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied by the browser; stay un-copied.
    }
  }

  return (
    <Collapsible.Root open={open} onOpenChange={setOpen} className="overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between gap-2 border-b border-border bg-surface-muted px-3 py-1.5">
        <Collapsible.Trigger asChild>
          <button type="button" className="flex min-h-11 items-center gap-1.5 text-sm font-medium text-text cursor-pointer">
            {open ? (
              <ChevronDown className="size-4 shrink-0" aria-hidden="true" />
            ) : (
              <ChevronRight className="size-4 shrink-0" aria-hidden="true" />
            )}
            {label}
          </button>
        </Collapsible.Trigger>
        <button
          type="button"
          onClick={handleCopy}
          aria-label={copied ? `Copied ${label}` : `Copy ${label}`}
          className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface hover:text-text cursor-pointer"
        >
          {copied ? (
            <Check className="size-3.5 text-success-solid" aria-hidden="true" />
          ) : (
            <Copy className="size-3.5" aria-hidden="true" />
          )}
        </button>
      </div>
      <Collapsible.Content>
        {value ? (
          <pre className="max-h-96 overflow-auto p-3 font-mono text-[13px] leading-relaxed whitespace-pre-wrap text-text">
            {lines.map((line, index) => (
              <CodeLine key={index} line={line} />
            ))}
          </pre>
        ) : (
          <p className="p-3 text-sm text-text-muted">Empty</p>
        )}
      </Collapsible.Content>
    </Collapsible.Root>
  );
}

function CodeLine({ line }: { line: string }) {
  if (!line.includes(REDACTED_PLACEHOLDER)) {
    return <div>{line || " "}</div>;
  }
  const markerIndex = line.indexOf(REDACTED_PLACEHOLDER);
  const before = line.slice(0, markerIndex);
  const after = line.slice(markerIndex + REDACTED_PLACEHOLDER.length);
  return (
    <div className="-mx-1 rounded-sm bg-warning-bg px-1">
      {before}
      <span className="font-semibold text-warning-text">{REDACTED_PLACEHOLDER}</span>
      {after}
    </div>
  );
}

function prettyPrintJson(value: string): string {
  if (!value) {
    return "";
  }
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}
