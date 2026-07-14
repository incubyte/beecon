import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DetailRow } from "@/components/ui/DetailRow";
import { Drawer } from "@/components/ui/Drawer";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { HttpStatusBadge } from "@/components/ui/HttpStatusBadge";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import { ApiError } from "@/lib/api-client";
import type { DeliveryEvent } from "@/lib/api-types";

import { useEventDeliveryAttempts, useRedeliverEvent } from "./api";

export interface EventDrawerProps {
  orgId: string;
  event: DeliveryEvent | null;
  onClose: () => void;
}

const REDELIVERABLE_STATUSES = new Set(["FAILED", "NO_ENDPOINT"]);

/** EventDrawer is Slice 3's right-side detail panel for one outbox event
 * (AC2/AC3): id, type, delivery-status pill, attempt count, timestamps, a
 * per-attempt history table (attempt number, response status, duration),
 * and a manual Redeliver action for a FAILED or NO_ENDPOINT event. */
export function EventDrawer({ orgId, event, onClose }: EventDrawerProps) {
  const {
    data: attempts,
    isLoading: attemptsLoading,
    isError: attemptsError,
  } = useEventDeliveryAttempts(orgId, event?.id);
  const redeliver = useRedeliverEvent(orgId);

  const canRedeliver = Boolean(event && REDELIVERABLE_STATUSES.has(event.deliveryStatus));

  return (
    <Drawer
      open={event !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Event detail"
      description={event ? <CopyIdChip id={event.id} /> : undefined}
    >
      {event ? (
        <div className="flex flex-col gap-5">
          <dl className="flex flex-col gap-4">
            <DetailRow label="Type">
              <span className="font-mono text-sm text-text">{event.type}</span>
            </DetailRow>
            <DetailRow label="Status">
              <StatusBadge taxonomy="event" status={event.deliveryStatus} />
            </DetailRow>
            <DetailRow label="Attempts">
              <span className="text-text">{event.attempts}</span>
            </DetailRow>
            <DetailRow label="Created">
              <Timestamp iso={event.createdAt} />
            </DetailRow>
            {event.lastAttemptAt ? (
              <DetailRow label="Last attempt">
                <Timestamp iso={event.lastAttemptAt} />
              </DetailRow>
            ) : null}
          </dl>

          <div className="flex flex-col gap-2 border-t border-border pt-4">
            <button
              type="button"
              onClick={() => redeliver.mutate(event.id)}
              disabled={!canRedeliver || redeliver.isPending}
              className="min-h-11 self-start rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {redeliver.isPending ? "Queuing…" : "Redeliver"}
            </button>
            {!canRedeliver ? (
              <p className="text-xs text-text-muted">Only a FAILED or NO_ENDPOINT event can be redelivered.</p>
            ) : null}
            {redeliver.isSuccess ? (
              <p className="text-xs text-success-text">Redelivery queued — a new attempt will appear below shortly.</p>
            ) : null}
            {redeliver.isError ? <p className="text-xs text-error-text">{errorMessage(redeliver.error)}</p> : null}
          </div>

          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium text-text">Delivery attempts</p>
            {attemptsError ? (
              <ErrorCard message="Delivery attempt history could not be loaded." />
            ) : attemptsLoading ? (
              <p className="text-sm text-text-secondary">Loading…</p>
            ) : attempts && attempts.length > 0 ? (
              <table className="w-full border-collapse text-left text-sm">
                <thead>
                  <tr>
                    <th scope="col" className="border-b border-border py-2 text-xs font-medium tracking-wide text-text-muted uppercase">
                      Attempt
                    </th>
                    <th scope="col" className="border-b border-border py-2 text-xs font-medium tracking-wide text-text-muted uppercase">
                      Status
                    </th>
                    <th scope="col" className="border-b border-border py-2 text-xs font-medium tracking-wide text-text-muted uppercase">
                      Duration
                    </th>
                    <th scope="col" className="border-b border-border py-2 text-xs font-medium tracking-wide text-text-muted uppercase">
                      Time
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {attempts.map((attempt) => (
                    <tr key={attempt.id} className="border-b border-border last:border-b-0">
                      <td className="py-2 text-text">{attempt.attempt}</td>
                      <td className="py-2">
                        <HttpStatusBadge status={attempt.status} />
                      </td>
                      <td className="py-2 font-mono text-xs text-text-secondary">{attempt.durationMs} ms</td>
                      <td className="py-2">
                        <Timestamp iso={attempt.createdAt} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="text-sm text-text-muted">No delivery attempts recorded yet.</p>
            )}
          </div>
        </div>
      ) : null}
    </Drawer>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The redeliver request failed.";
  }
  return "The redeliver request failed.";
}
