import { useEffect, useId, useState } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";
import type { WebhookEndpoint } from "@/lib/api-types";

import { useUpdateWebhookEndpoint } from "./api";
import { EventTypeFilterEditor } from "./EventTypeFilterEditor";

export interface EditWebhookEndpointModalProps {
  orgId: string;
  endpoint: WebhookEndpoint | null;
  onClose: () => void;
}

/** EditWebhookEndpointModal is Slice 8's URL/event-type-filter editor
 * (AC3): a whole-object PUT, mirroring GovernancePage's own replace
 * convention — both fields are always sent together, pre-filled from the
 * endpoint being edited. Rendering is gated on `endpoint !== null` so the
 * form always mounts with fresh initial state per endpoint. */
export function EditWebhookEndpointModal({ orgId, endpoint, onClose }: EditWebhookEndpointModalProps) {
  const updateEndpoint = useUpdateWebhookEndpoint(orgId);
  const [url, setUrl] = useState("");
  const [eventTypes, setEventTypes] = useState<string[] | null>(null);
  const urlInputId = useId();

  useEffect(() => {
    if (endpoint) {
      setUrl(endpoint.url);
      setEventTypes(endpoint.eventTypes);
      updateEndpoint.reset();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [endpoint]);

  if (!endpoint) {
    return null;
  }

  function handleOpenChange(next: boolean) {
    if (!next) {
      onClose();
    }
  }

  function handleSubmit() {
    if (!endpoint) {
      return;
    }
    updateEndpoint.mutate({ wepId: endpoint.id, url, eventTypes }, { onSuccess: () => onClose() });
  }

  const canSubmit = url.trim() !== "" && (eventTypes === null || eventTypes.length > 0);

  return (
    <Modal open={endpoint !== null} onOpenChange={handleOpenChange} title="Edit webhook endpoint" description={endpoint.id}>
      <div className="flex flex-col gap-4">
        <label htmlFor={urlInputId} className="flex flex-col gap-1.5 text-sm text-text">
          URL
          <input
            id={urlInputId}
            type="url"
            value={url}
            onChange={(event) => setUrl(event.target.value)}
            autoComplete="off"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        <EventTypeFilterEditor value={eventTypes} onChange={setEventTypes} />

        {updateEndpoint.isError ? <p className="text-sm text-error-text">{errorMessage(updateEndpoint.error)}</p> : null}

        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!canSubmit || updateEndpoint.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {updateEndpoint.isPending ? "Saving…" : "Save changes"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The webhook endpoint could not be updated.";
  }
  return "The webhook endpoint could not be updated.";
}
