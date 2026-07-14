import { useSearch } from "@tanstack/react-router";
import { useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SecretOnceModal } from "@/components/ui/SecretOnceModal";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { IssuedApiKey, RotatedApiKey } from "@/lib/api-types";

import { useApiKeys } from "./api";
import { buildApiKeyColumns } from "./columns";
import { CreateApiKeyModal } from "./CreateApiKeyModal";

/** ApiKeysPage is Slice 4's Administer > API Keys surface: the selected
 * org's keys (AC3), a create flow with a scope choice whose secret is shown
 * exactly once (AC4), and per-row rotate (also shown once, AC6) / revoke
 * (TypeToConfirm) actions. The console never writes a secret anywhere but
 * this one-time modal — not to the query cache, not to an error message,
 * not to a log (AC7). */
export function ApiKeysPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const { data: apiKeys, isLoading, isError, error, refetch } = useApiKeys(orgId);

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [revealedSecret, setRevealedSecret] = useState<{ key: string; label: string } | null>(null);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its API keys."
      />
    );
  }

  function handleIssued(issued: IssuedApiKey) {
    setRevealedSecret({ key: issued.key, label: `New ${issued.scope} API key` });
  }

  function handleRotated(rotated: RotatedApiKey) {
    setRevealedSecret({ key: rotated.key, label: "Rotated API key" });
  }

  const columns = buildApiKeyColumns(orgId, handleRotated);
  const items = apiKeys ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-text">API Keys</h1>
          <p className="text-sm text-text-secondary">The selected organization's server API keys.</p>
        </div>
        <button
          type="button"
          onClick={() => setIsCreateOpen(true)}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
        >
          Create API key
        </button>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <DataTable
          caption="API keys"
          columns={columns}
          data={items}
          isLoading={isLoading}
          loadingRows={<SkeletonRows columns={columns.length} />}
          emptyState={
            <EmptyState
              title="No API keys yet"
              description="Create the first key to let this organization's server authenticate against Beecon."
            />
          }
        />
      )}

      <CreateApiKeyModal orgId={orgId} open={isCreateOpen} onOpenChange={setIsCreateOpen} onIssued={handleIssued} />

      <SecretOnceModal
        open={revealedSecret !== null}
        onDismiss={() => setRevealedSecret(null)}
        title={revealedSecret?.label ?? "API key secret"}
        secret={revealedSecret?.key ?? ""}
        helpText="This is the full secret. It authenticates as this key and will never be shown again."
        fileNamePrefix="beecon-api-key"
      />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The API keys list could not be loaded.";
  }
  return "The API keys list could not be loaded.";
}
