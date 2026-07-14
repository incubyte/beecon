import { useId, useState } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";
import type { CreatedWebhookEndpoint } from "@/lib/api-types";

import { useCreateWebhookEndpoint } from "./api";
import { EventTypeFilterEditor } from "./EventTypeFilterEditor";

export interface CreateWebhookEndpointModalProps {
  orgId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (created: CreatedWebhookEndpoint) => void;
}

/** CreateWebhookEndpointModal is Slice 8's AC1 create flow: a URL, an
 * optional event-type filter, then the freshly minted secret handed back
 * to the caller (WebhookEndpointsPage) so it can be shown exactly once via
 * SecretOnceModal — this modal itself never renders the secret. Registering
 * an endpoint beyond BEECON_WEBHOOK_ENDPOINT_CAP surfaces the backend's own
 * cap-naming validation error inline (AC2). */
export function CreateWebhookEndpointModal({ orgId, open, onOpenChange, onCreated }: CreateWebhookEndpointModalProps) {
  const createEndpoint = useCreateWebhookEndpoint(orgId);
  const [url, setUrl] = useState("");
  const [eventTypes, setEventTypes] = useState<string[] | null>(null);
  const urlInputId = useId();

  function handleOpenChange(next: boolean) {
    if (!next) {
      setUrl("");
      setEventTypes(null);
      createEndpoint.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit() {
    createEndpoint.mutate(
      { url, eventTypes },
      {
        onSuccess: (created) => {
          handleOpenChange(false);
          onCreated(created);
        },
      },
    );
  }

  const canSubmit = url.trim() !== "" && (eventTypes === null || eventTypes.length > 0);

  return (
    <Modal
      open={open}
      onOpenChange={handleOpenChange}
      title="Register webhook endpoint"
      description="The endpoint's own signing secret is generated server-side and shown once, immediately after creation."
    >
      <div className="flex flex-col gap-4">
        <label htmlFor={urlInputId} className="flex flex-col gap-1.5 text-sm text-text">
          URL
          <input
            id={urlInputId}
            type="url"
            value={url}
            onChange={(event) => setUrl(event.target.value)}
            placeholder="https://example.com/webhooks/beecon"
            autoComplete="off"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        <EventTypeFilterEditor value={eventTypes} onChange={setEventTypes} />

        {createEndpoint.isError ? <p className="text-sm text-error-text">{errorMessage(createEndpoint.error)}</p> : null}

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
            disabled={!canSubmit || createEndpoint.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {createEndpoint.isPending ? "Registering…" : "Register endpoint"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The webhook endpoint could not be created.";
  }
  return "The webhook endpoint could not be created.";
}
