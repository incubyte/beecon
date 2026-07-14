import { useState } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";
import type { ApiKeyScope, IssuedApiKey } from "@/lib/api-types";

import { useIssueApiKey } from "./api";

export interface CreateApiKeyModalProps {
  orgId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onIssued: (issued: IssuedApiKey) => void;
}

const SCOPE_OPTIONS: { value: ApiKeyScope; label: string; description: string }[] = [
  { value: "read-write", label: "Read-write", description: "Full access — can create, update, delete, and execute." },
  { value: "read-only", label: "Read-only", description: "Listing and inspection only; every mutating call is rejected." },
];

/** CreateApiKeyModal is Slice 4's AC4 create flow: choose a scope, issue the
 * key, then hand the full secret to the caller (ApiKeysPage) so it can be
 * shown exactly once via SecretOnceModal — this modal itself never renders
 * the secret, keeping the scope-choice and the credential-ceremony
 * concerns separate. */
export function CreateApiKeyModal({ orgId, open, onOpenChange, onIssued }: CreateApiKeyModalProps) {
  const issueApiKey = useIssueApiKey(orgId);
  const [scope, setScope] = useState<ApiKeyScope>("read-write");

  function handleOpenChange(next: boolean) {
    if (!next) {
      setScope("read-write");
      issueApiKey.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit() {
    issueApiKey.mutate(scope, {
      onSuccess: (issued) => {
        handleOpenChange(false);
        onIssued(issued);
      },
    });
  }

  return (
    <Modal
      open={open}
      onOpenChange={handleOpenChange}
      title="Create API key"
      description="Choose the key's scope. The full secret is shown once, immediately after creation."
    >
      <div className="flex flex-col gap-3">
        <fieldset className="flex flex-col gap-2">
          <legend className="sr-only">Scope</legend>
          {SCOPE_OPTIONS.map((option) => (
            <label
              key={option.value}
              className={`flex cursor-pointer items-start gap-2.5 rounded-md border p-3 text-sm transition-colors ${
                scope === option.value ? "border-primary bg-primary/5" : "border-border-strong"
              }`}
            >
              <input
                type="radio"
                name="scope"
                value={option.value}
                checked={scope === option.value}
                onChange={() => setScope(option.value)}
                className="mt-0.5 size-4 shrink-0 cursor-pointer"
              />
              <span>
                <span className="block font-medium text-text">{option.label}</span>
                <span className="block text-text-secondary">{option.description}</span>
              </span>
            </label>
          ))}
        </fieldset>

        {issueApiKey.isError ? <p className="text-sm text-error-text">{errorMessage(issueApiKey.error)}</p> : null}

        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => handleOpenChange(false)}
            className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={issueApiKey.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {issueApiKey.isPending ? "Creating…" : "Create key"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The API key could not be created.";
  }
  return "The API key could not be created.";
}
