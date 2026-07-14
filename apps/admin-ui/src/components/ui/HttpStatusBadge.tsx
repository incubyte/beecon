import { AlertTriangle, Check, XCircle } from "lucide-react";

export interface HttpStatusBadgeProps {
  status: number;
  size?: "sm" | "md";
}

/** HttpStatusBadge renders a raw upstream HTTP status code as icon + color +
 * text (DESIGN.md §9: color is never the only signal) — used everywhere a
 * log entry or delivery attempt shows its outcome (Slice 3): 0 (the
 * provider was never reached at all) reads as "Unreachable", 2xx as
 * success, everything else as an error. Not a StatusBadge taxonomy entry
 * because a raw status code isn't a fixed enum. */
export function HttpStatusBadge({ status, size = "sm" }: HttpStatusBadgeProps) {
  const textSize = size === "sm" ? "text-xs" : "text-sm";
  const iconSize = size === "sm" ? "size-3.5" : "size-4";

  if (status === 0) {
    return (
      <span className={`inline-flex items-center gap-1.5 font-mono ${textSize} text-error-text`}>
        <XCircle className={`${iconSize} shrink-0`} aria-hidden="true" />
        Unreachable
      </span>
    );
  }
  const isSuccess = status >= 200 && status < 300;
  const Icon = isSuccess ? Check : AlertTriangle;
  const textClass = isSuccess ? "text-success-text" : "text-error-text";
  return (
    <span className={`inline-flex items-center gap-1.5 font-mono ${textSize} ${textClass}`}>
      <Icon className={`${iconSize} shrink-0`} aria-hidden="true" />
      {status}
    </span>
  );
}
