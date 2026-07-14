import {
  AlertTriangle,
  Check,
  Clock,
  Eye,
  HelpCircle,
  Pause,
  Pencil,
  RotateCw,
  Skull,
  XCircle,
  type LucideIcon,
} from "lucide-react";

export type StatusTaxonomy =
  | "connection"
  | "triggerInstance"
  | "event"
  | "apiKey"
  | "apiKeyScope"
  | "integrationVisibility"
  | "endpoint";

interface StatusVisual {
  label: string;
  icon: LucideIcon;
  textClass: string;
  bgClass: string;
}

/** Connection taxonomy (DESIGN.md §7): ACTIVE/INITIATED/DISCONNECTED/EXPIRED,
 * each pairing a semantic color with an icon and a text label — color is
 * never the only signal (Slice 2, AC1). */
const connectionStatuses: Record<string, StatusVisual> = {
  ACTIVE: { label: "Active", icon: Check, textClass: "text-success-text", bgClass: "bg-success-bg" },
  INITIATED: { label: "Initiated", icon: Clock, textClass: "text-info-text", bgClass: "bg-info-bg" },
  DISCONNECTED: { label: "Disconnected", icon: XCircle, textClass: "text-error-text", bgClass: "bg-error-bg" },
  EXPIRED: { label: "Expired", icon: AlertTriangle, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
};

/** Trigger-instance taxonomy (DESIGN.md §7): ACTIVE/DISABLED/ERROR. ERROR is
 * carried in the taxonomy for forward compatibility with the design brief's
 * documented states even though the current backend (Slice 2) only ever
 * returns ACTIVE/DISABLED — an unrecognized status still renders through the
 * neutral fallback below rather than crashing. */
const triggerInstanceStatuses: Record<string, StatusVisual> = {
  ACTIVE: { label: "Active", icon: Check, textClass: "text-success-text", bgClass: "bg-success-bg" },
  DISABLED: { label: "Disabled", icon: Pause, textClass: "text-neutral-text", bgClass: "bg-neutral-bg" },
  ERROR: { label: "Error", icon: AlertTriangle, textClass: "text-error-text", bgClass: "bg-error-bg" },
};

/** Webhook / event delivery taxonomy (DESIGN.md §7): the backend returns
 * only PENDING/DELIVERED/FAILED/NO_ENDPOINT today (delivery/types.go's
 * Status enum) — RETRYING/DEAD are carried here for forward compatibility
 * with the design brief's full documented taxonomy, the same precedent
 * triggerInstance's ERROR entry set (Slice 2). */
const eventStatuses: Record<string, StatusVisual> = {
  DELIVERED: { label: "Delivered", icon: Check, textClass: "text-success-text", bgClass: "bg-success-bg" },
  PENDING: { label: "Pending", icon: Clock, textClass: "text-neutral-text", bgClass: "bg-neutral-bg" },
  RETRYING: { label: "Retrying", icon: RotateCw, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
  FAILED: { label: "Failed", icon: XCircle, textClass: "text-error-text", bgClass: "bg-error-bg" },
  DEAD: { label: "Dead", icon: Skull, textClass: "text-error-text", bgClass: "bg-error-bg" },
  NO_ENDPOINT: { label: "No endpoint", icon: AlertTriangle, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
};

/** API-key taxonomy (DESIGN.md §7, Slice 4): ACTIVE/ROTATING (grace
 * period)/REVOKED/EXPIRED — derived client-side (see format.ts's
 * deriveApiKeyStatus) from the key's revokedAt/rotatedAt/overlapExpiresAt
 * fields, since the backend carries those as raw timestamps rather than a
 * single status enum. */
const apiKeyStatuses: Record<string, StatusVisual> = {
  ACTIVE: { label: "Active", icon: Check, textClass: "text-success-text", bgClass: "bg-success-bg" },
  ROTATING: { label: "Rotating", icon: RotateCw, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
  REVOKED: { label: "Revoked", icon: XCircle, textClass: "text-error-text", bgClass: "bg-error-bg" },
  EXPIRED: { label: "Expired", icon: AlertTriangle, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
};

/** API-key scope taxonomy (PD41, Slice 4): read-only/read-write, the same
 * color+icon+text pill treatment as every other status — scope is not a
 * lifecycle state, but reusing StatusBadge keeps every key attribute
 * legible in grayscale and consistent with the rest of the console. */
const apiKeyScopeStatuses: Record<string, StatusVisual> = {
  "read-only": { label: "Read-only", icon: Eye, textClass: "text-info-text", bgClass: "bg-info-bg" },
  "read-write": { label: "Read-write", icon: Pencil, textClass: "text-success-text", bgClass: "bg-success-bg" },
};

/** Integration-visibility taxonomy (Slice 5, AC1): VISIBLE/HIDDEN/NOT_ALLOWED
 * — the operator's per-org effective-visibility view over the whole catalog
 * (catalog.IntegrationVisibility's Visibility field), the same
 * color+icon+text pill treatment as every other status. */
const integrationVisibilityStatuses: Record<string, StatusVisual> = {
  VISIBLE: { label: "Visible", icon: Eye, textClass: "text-success-text", bgClass: "bg-success-bg" },
  HIDDEN: { label: "Hidden", icon: Pause, textClass: "text-warning-text", bgClass: "bg-warning-bg" },
  NOT_ALLOWED: { label: "Not allowed", icon: XCircle, textClass: "text-error-text", bgClass: "bg-error-bg" },
};

/** Webhook-endpoint taxonomy (DESIGN.md §7, Slice 8, PD45):
 * ENABLED/DISABLED/DISABLED_AUTO — DISABLED_AUTO is the auto-disable
 * bookkeeping's own outcome (dispatchOne, delivery/facade.go) after too
 * many consecutive terminal FAILED deliveries, distinct from an operator's
 * own DISABLED. */
const endpointStatuses: Record<string, StatusVisual> = {
  ENABLED: { label: "Enabled", icon: Check, textClass: "text-success-text", bgClass: "bg-success-bg" },
  DISABLED: { label: "Disabled", icon: Pause, textClass: "text-neutral-text", bgClass: "bg-neutral-bg" },
  DISABLED_AUTO: { label: "Auto-disabled", icon: AlertTriangle, textClass: "text-error-text", bgClass: "bg-error-bg" },
};

const taxonomies: Record<StatusTaxonomy, Record<string, StatusVisual>> = {
  connection: connectionStatuses,
  triggerInstance: triggerInstanceStatuses,
  event: eventStatuses,
  apiKey: apiKeyStatuses,
  apiKeyScope: apiKeyScopeStatuses,
  integrationVisibility: integrationVisibilityStatuses,
  endpoint: endpointStatuses,
};

export interface StatusBadgeProps {
  taxonomy: StatusTaxonomy;
  status: string;
}

/** StatusBadge is the single pill every status taxonomy renders through
 * (DESIGN.md §7/§9): a tinted background, a leading icon, and a text label,
 * legible in grayscale and under color-vision deficiency. An unrecognized
 * status value (never expected, but never fatal) falls back to a neutral
 * pill carrying the raw value as its label. */
export function StatusBadge({ taxonomy, status }: StatusBadgeProps) {
  const visual = taxonomies[taxonomy][status] ?? {
    label: status,
    icon: HelpCircle,
    textClass: "text-neutral-text",
    bgClass: "bg-neutral-bg",
  };
  const Icon = visual.icon;

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-pill px-2.5 py-1 text-xs font-medium ${visual.bgClass} ${visual.textClass}`}
    >
      <Icon className="size-3.5 shrink-0" aria-hidden="true" />
      {visual.label}
    </span>
  );
}
