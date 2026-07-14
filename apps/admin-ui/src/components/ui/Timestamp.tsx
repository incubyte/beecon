import { formatAbsolute, formatRelativeTime } from "@/lib/format";

export interface TimestampProps {
  iso: string;
}

/** Timestamp renders a machine timestamp as relative text with the absolute
 * value on hover (DESIGN.md §6: "timestamps show relative text with the
 * absolute value on hover") — used for connection and trigger-instance
 * created/updated columns and detail rows (Slice 2, AC2). The native
 * `title` attribute carries the absolute value so it works for mouse hover
 * and is still available to assistive tech via the element's accessible
 * description. */
export function Timestamp({ iso }: TimestampProps) {
  return (
    <time dateTime={iso} title={formatAbsolute(iso)} className="text-text-secondary">
      {formatRelativeTime(iso)}
    </time>
  );
}
