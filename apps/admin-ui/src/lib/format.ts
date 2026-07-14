/** formatDate renders an RFC3339 timestamp as a short, locale-aware date —
 * used for machine timestamps that don't yet need the relative/absolute
 * hover treatment Slice 2's connection/trigger-instance timestamps add. */
export function formatDate(iso: string): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return parsed.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/** truncateId shortens a long CUID2 id (org_..., conn_..., tool_...) to a
 * scannable prefix + suffix for display next to a CopyIdChip's full-id copy
 * action (DESIGN.md §6/§7). */
export function truncateId(id: string, headChars = 10, tailChars = 4): string {
  if (id.length <= headChars + tailChars + 1) {
    return id;
  }
  return `${id.slice(0, headChars)}…${id.slice(-tailChars)}`;
}

const RELATIVE_UNIT_SECONDS: [unit: Intl.RelativeTimeFormatUnit, secondsInUnit: number][] = [
  ["year", 31536000],
  ["month", 2592000],
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
  ["second", 1],
];

/** formatRelativeTime renders an RFC3339 timestamp as short relative text
 * ("3 hours ago") — the Timestamp component (DESIGN.md §6) pairs this with
 * the absolute value on hover so an operator scanning a table still has the
 * exact instant one glance away. Falls back to the raw string for an
 * unparseable timestamp rather than throwing. */
export function formatRelativeTime(iso: string, now: Date = new Date()): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  const diffSeconds = (parsed.getTime() - now.getTime()) / 1000;
  const [unit, secondsInUnit] = pickRelativeUnit(diffSeconds);
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  return formatter.format(Math.round(diffSeconds / secondsInUnit), unit);
}

function pickRelativeUnit(diffSeconds: number): [Intl.RelativeTimeFormatUnit, number] {
  const absSeconds = Math.abs(diffSeconds);
  for (const entry of RELATIVE_UNIT_SECONDS) {
    const [, secondsInUnit] = entry;
    if (absSeconds >= secondsInUnit || secondsInUnit === 1) {
      return entry;
    }
  }
  return ["second", 1];
}

/** formatDurationSeconds renders a duration in seconds as short human text
 * (Slice 3's dashboard: the outbox's oldest-pending-event age) — the
 * largest whole unit that fits, so "0" reads as "0s" rather than "0h". */
export function formatDurationSeconds(totalSeconds: number): string {
  const seconds = Math.max(0, Math.round(totalSeconds));
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

/** formatAbsolute renders an RFC3339 timestamp as a full local date+time —
 * the hover value the Timestamp component pairs with formatRelativeTime's
 * short text (DESIGN.md §6). */
export function formatAbsolute(iso: string): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) {
    return iso;
  }
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
