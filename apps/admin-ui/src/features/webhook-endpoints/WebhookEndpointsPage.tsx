import { useSearch } from "@tanstack/react-router";
import { useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SecretOnceModal } from "@/components/ui/SecretOnceModal";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { CreatedWebhookEndpoint, RotatedWebhookEndpointSecret, WebhookEndpoint } from "@/lib/api-types";

import { useWebhookEndpoints } from "./api";
import { buildWebhookEndpointColumns } from "./columns";
import { CreateWebhookEndpointModal } from "./CreateWebhookEndpointModal";
import { EditWebhookEndpointModal } from "./EditWebhookEndpointModal";

/** WebhookEndpointsPage is Slice 8's GOVERN > Settings > Webhook Endpoints
 * surface (PD45): the selected org's endpoints (URL, event-type filter,
 * status incl. DISABLED_AUTO, consecutive failures), a create flow capped
 * at BEECON_WEBHOOK_ENDPOINT_CAP whose secret is shown exactly once
 * (AC1/AC2), and per-row edit/enable-disable/rotate/delete actions (AC3,
 * AC6, AC8). The console never writes a secret anywhere but the one-time
 * modal — not to the query cache, not to an error message, not to a log,
 * mirroring ApiKeysPage's own credential-handling discipline. */
export function WebhookEndpointsPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const { data: endpoints, isLoading, isError, error, refetch } = useWebhookEndpoints(orgId);

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState<WebhookEndpoint | null>(null);
  const [revealedSecret, setRevealedSecret] = useState<{ key: string; label: string } | null>(null);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its webhook endpoints."
      />
    );
  }

  function handleCreated(created: CreatedWebhookEndpoint) {
    setRevealedSecret({ key: created.secret, label: "New webhook endpoint secret" });
  }

  function handleRotated(rotated: RotatedWebhookEndpointSecret) {
    setRevealedSecret({ key: rotated.secret, label: "Rotated webhook endpoint secret" });
  }

  const columns = buildWebhookEndpointColumns(orgId, handleRotated, setEditingEndpoint);
  const items = endpoints ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-text">Webhook Endpoints</h1>
          <p className="text-sm text-text-secondary">
            The selected organization's webhook receivers. Each has its own signing secret and optional event-type
            filter; an endpoint that keeps failing auto-disables.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setIsCreateOpen(true)}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
        >
          Register endpoint
        </button>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <DataTable
          caption="Webhook endpoints"
          columns={columns}
          data={items}
          isLoading={isLoading}
          loadingRows={<SkeletonRows columns={columns.length} />}
          emptyState={
            <EmptyState
              title="No webhook endpoints yet"
              description="Register the first endpoint so this organization can receive signed webhook deliveries."
            />
          }
        />
      )}

      <CreateWebhookEndpointModal orgId={orgId} open={isCreateOpen} onOpenChange={setIsCreateOpen} onCreated={handleCreated} />

      <EditWebhookEndpointModal orgId={orgId} endpoint={editingEndpoint} onClose={() => setEditingEndpoint(null)} />

      <SecretOnceModal
        open={revealedSecret !== null}
        onDismiss={() => setRevealedSecret(null)}
        title={revealedSecret?.label ?? "Webhook endpoint secret"}
        secret={revealedSecret?.key ?? ""}
        helpText="This is the full whsec_ signing secret. It authenticates this endpoint's deliveries and will never be shown again."
        fileNamePrefix="beecon-webhook-secret"
      />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The webhook endpoints list could not be loaded.";
  }
  return "The webhook endpoints list could not be loaded.";
}
