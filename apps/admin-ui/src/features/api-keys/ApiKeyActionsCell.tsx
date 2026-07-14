import { TypeToConfirm } from "@/components/ui/TypeToConfirm";
import { ApiError } from "@/lib/api-client";
import type { ApiKeyListing, RotatedApiKey } from "@/lib/api-types";

import { useRevokeApiKey, useRotateApiKey } from "./api";

export interface ApiKeyActionsCellProps {
  orgId: string;
  apiKey: ApiKeyListing;
  onRotated: (rotated: RotatedApiKey) => void;
}

/** ApiKeyActionsCell is Slice 4's per-row rotate/revoke actions (AC6): a
 * revoked key has nothing left to act on; rotate mints a fresh secret shown
 * once by the caller's SecretOnceModal; revoke goes through TypeToConfirm —
 * the highest-risk destructive action in this feature area, matching
 * ConnectionDrawer's own delete precedent. */
export function ApiKeyActionsCell({ orgId, apiKey, onRotated }: ApiKeyActionsCellProps) {
  const rotateApiKey = useRotateApiKey(orgId);
  const revokeApiKey = useRevokeApiKey(orgId);
  const isRevoked = Boolean(apiKey.revokedAt);

  function handleRotate() {
    rotateApiKey.mutate(apiKey.id, { onSuccess: onRotated });
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={handleRotate}
          disabled={isRevoked || rotateApiKey.isPending}
          className="min-h-11 rounded-md border border-border-strong px-3 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {rotateApiKey.isPending ? "Rotating…" : "Rotate"}
        </button>

        <TypeToConfirm
          trigger={
            <button
              type="button"
              disabled={isRevoked}
              className="min-h-11 rounded-md border border-error-solid/40 px-3 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              Revoke
            </button>
          }
          title="Revoke this API key?"
          description="Every secret ever issued for this key stops authenticating immediately. This cannot be undone."
          confirmText={apiKey.id}
          confirmLabel="Revoke key"
          onConfirm={() => revokeApiKey.mutate(apiKey.id)}
          isConfirming={revokeApiKey.isPending}
        />
      </div>
      {rotateApiKey.isError ? <p className="text-xs text-error-text">{errorMessage(rotateApiKey.error)}</p> : null}
      {revokeApiKey.isError ? <p className="text-xs text-error-text">{errorMessage(revokeApiKey.error)}</p> : null}
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The request failed.";
  }
  return "The request failed.";
}
