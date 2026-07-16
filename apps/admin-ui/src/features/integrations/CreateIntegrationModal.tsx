import { useId, useState, type FormEvent } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";
import type { IntegrationSummary } from "@/lib/api-types";

import { useCreateIntegration } from "./api";

export interface CreateIntegrationModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (integration: IntegrationSummary) => void;
  /** The provider this integration is created against — locked, not
   * chosen: the modal always opens from a specific provider's detail page
   * (this slice moved "add integration" off the installation-wide
   * Providers list). */
  providerSlug: string;
  providerName: string;
}

/** CreateIntegrationModal is the operator's create-integration flow, scoped
 * to one provider: supply its OAuth client id and secret, and register the
 * installation integration against providerSlug. Like CreateOperatorModal
 * (and unlike CreateApiKeyModal), the credential is operator-supplied, not
 * server-minted — the clientSecret is write-once and the response never
 * returns it, so this modal never renders any secret back. */
export function CreateIntegrationModal({ open, onOpenChange, onCreated, providerSlug, providerName }: CreateIntegrationModalProps) {
  const createIntegration = useCreateIntegration();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const clientIdFieldId = useId();
  const clientSecretFieldId = useId();

  function handleOpenChange(next: boolean) {
    if (!next) {
      setClientId("");
      setClientSecret("");
      createIntegration.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    createIntegration.mutate(
      { providerSlug, clientId, clientSecret },
      {
        onSuccess: (integration) => {
          handleOpenChange(false);
          onCreated(integration);
        },
      },
    );
  }

  const canSubmit = Boolean(clientId.trim() && clientSecret) && !createIntegration.isPending;

  return (
    <Modal
      open={open}
      onOpenChange={handleOpenChange}
      title="Create integration"
      description="Register an integration with this provider's OAuth client credentials. The client secret is stored once and is never shown again."
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <div className="flex flex-col gap-1 text-sm text-text-secondary">
          Provider
          <p className="min-h-11 flex items-center rounded-md border border-border bg-surface-muted px-3 text-sm text-text">
            {providerName}
          </p>
        </div>

        <label htmlFor={clientIdFieldId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Client ID
          <input
            id={clientIdFieldId}
            type="text"
            value={clientId}
            onChange={(event) => setClientId(event.target.value)}
            required
            autoFocus
            autoComplete="off"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        <label htmlFor={clientSecretFieldId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Client secret
          <input
            id={clientSecretFieldId}
            type="password"
            value={clientSecret}
            onChange={(event) => setClientSecret(event.target.value)}
            required
            autoComplete="off"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        {createIntegration.isError ? (
          <p className="text-sm text-error-text">{errorMessage(createIntegration.error)}</p>
        ) : null}

        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => handleOpenChange(false)}
            className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {createIntegration.isPending ? "Creating…" : "Create integration"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The integration could not be created.";
  }
  return "The integration could not be created.";
}
