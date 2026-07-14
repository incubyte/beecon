import { Pencil } from "lucide-react";

import { TypeToConfirm } from "@/components/ui/TypeToConfirm";
import { ApiError } from "@/lib/api-client";
import type { RotatedWebhookEndpointSecret, WebhookEndpoint } from "@/lib/api-types";

import { useDeleteWebhookEndpoint, useDisableWebhookEndpoint, useEnableWebhookEndpoint, useRotateWebhookEndpointSecret } from "./api";

export interface WebhookEndpointActionsCellProps {
  orgId: string;
  endpoint: WebhookEndpoint;
  onRotated: (rotated: RotatedWebhookEndpointSecret) => void;
  onEdit: (endpoint: WebhookEndpoint) => void;
}

/** WebhookEndpointActionsCell is Slice 8's per-row actions: edit
 * (URL/event-type filter), enable/disable (AC6), rotate-secret (AC8, shown
 * once by the caller's SecretOnceModal), and delete (AC8, guarded by
 * TypeToConfirm — the highest-risk destructive action in this feature
 * area, mirroring ApiKeyActionsCell's own revoke precedent). */
export function WebhookEndpointActionsCell({ orgId, endpoint, onRotated, onEdit }: WebhookEndpointActionsCellProps) {
  const rotateSecret = useRotateWebhookEndpointSecret(orgId);
  const enableEndpoint = useEnableWebhookEndpoint(orgId);
  const disableEndpoint = useDisableWebhookEndpoint(orgId);
  const deleteEndpoint = useDeleteWebhookEndpoint(orgId);

  const isEnabled = endpoint.status === "ENABLED";

  function handleRotate() {
    rotateSecret.mutate(endpoint.id, { onSuccess: onRotated });
  }

  function handleToggleEnabled() {
    if (isEnabled) {
      disableEndpoint.mutate(endpoint.id);
    } else {
      enableEndpoint.mutate(endpoint.id);
    }
  }

  const toggleBusy = enableEndpoint.isPending || disableEndpoint.isPending;

  return (
    <div className="flex flex-col items-start gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => onEdit(endpoint)}
          aria-label={`Edit endpoint ${endpoint.id}`}
          className="flex min-h-11 items-center gap-1.5 rounded-md border border-border-strong px-3 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
        >
          <Pencil className="size-3.5 shrink-0" aria-hidden="true" />
          Edit
        </button>

        <button
          type="button"
          onClick={handleToggleEnabled}
          disabled={toggleBusy}
          className="min-h-11 rounded-md border border-border-strong px-3 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {toggleBusy ? "Working…" : isEnabled ? "Disable" : "Enable"}
        </button>

        <button
          type="button"
          onClick={handleRotate}
          disabled={rotateSecret.isPending}
          className="min-h-11 rounded-md border border-border-strong px-3 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {rotateSecret.isPending ? "Rotating…" : "Rotate secret"}
        </button>

        <TypeToConfirm
          trigger={
            <button
              type="button"
              className="min-h-11 rounded-md border border-error-solid/40 px-3 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 cursor-pointer"
            >
              Delete
            </button>
          }
          title="Delete this webhook endpoint?"
          description="This endpoint stops receiving deliveries immediately. This cannot be undone."
          confirmText={endpoint.id}
          confirmLabel="Delete endpoint"
          onConfirm={() => deleteEndpoint.mutate(endpoint.id)}
          isConfirming={deleteEndpoint.isPending}
        />
      </div>
      {rotateSecret.isError ? <p className="text-xs text-error-text">{errorMessage(rotateSecret.error)}</p> : null}
      {enableEndpoint.isError ? <p className="text-xs text-error-text">{errorMessage(enableEndpoint.error)}</p> : null}
      {disableEndpoint.isError ? <p className="text-xs text-error-text">{errorMessage(disableEndpoint.error)}</p> : null}
      {deleteEndpoint.isError ? <p className="text-xs text-error-text">{errorMessage(deleteEndpoint.error)}</p> : null}
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The request failed.";
  }
  return "The request failed.";
}
