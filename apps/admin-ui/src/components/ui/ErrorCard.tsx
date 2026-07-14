import { AlertTriangle } from "lucide-react";

export interface ErrorCardProps {
  message: string;
  onRetry?: () => void;
}

/** ErrorCard is the first-class inline error state every list needs
 * (DESIGN.md §7/§9): icon + text, never color-only, with a Retry action. */
export function ErrorCard({ message, onRetry }: ErrorCardProps) {
  return (
    <div className="flex items-center justify-between gap-4 rounded-lg border border-error-solid/30 bg-error-bg px-4 py-3">
      <div className="flex items-center gap-2 text-error-text">
        <AlertTriangle className="size-4 shrink-0" aria-hidden="true" />
        <p className="text-sm">{message}</p>
      </div>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="min-h-11 rounded-md border border-error-solid/40 px-3 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 cursor-pointer"
        >
          Retry
        </button>
      ) : null}
    </div>
  );
}
