import { useSearch } from "@tanstack/react-router";
import { useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { FilterBar } from "@/components/ui/FilterBar";
import { FilterChip } from "@/components/ui/FilterChip";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { DeliveryEvent, EventDeliveryStatus } from "@/lib/api-types";

import { emptyEventsFilters, useEvents, type EventsFilters } from "./api";
import { eventColumns } from "./columns";
import { EventDrawer } from "./EventDrawer";

const EVENT_TYPES = ["trigger.event", "connection.expired", "webhook.test"];
const DELIVERY_STATUSES: EventDeliveryStatus[] = ["PENDING", "DELIVERED", "FAILED", "NO_ENDPOINT"];

/** EventsPage is Slice 3's OBSERVE > Events & Delivery surface (AC2/AC7):
 * the selected org's outbox events, filterable by type and delivery status
 * (each an individually-removable chip), cursor-paginated (load-more), a
 * row opens the detail drawer with per-attempt history and Redeliver. */
export function EventsPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;

  const [filters, setFilters] = useState<EventsFilters>(emptyEventsFilters);
  const [selectedEvent, setSelectedEvent] = useState<DeliveryEvent | null>(null);

  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useEvents(orgId, filters);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its events."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Events & Delivery</h1>
        <p className="text-sm text-text-secondary">Every outbox event and its webhook delivery status for the selected organization.</p>
      </div>

      <FilterBar>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          Type
          <select
            value={filters.type}
            onChange={(event) => setFilters((current) => ({ ...current, type: event.target.value }))}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          >
            <option value="">All types</option>
            {EVENT_TYPES.map((type) => (
              <option key={type} value={type}>
                {type}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          Status
          <select
            value={filters.deliveryStatus}
            onChange={(event) => setFilters((current) => ({ ...current, deliveryStatus: event.target.value }))}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          >
            <option value="">All statuses</option>
            {DELIVERY_STATUSES.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
        </label>
        {filters.type ? (
          <FilterChip label={`Type: ${filters.type}`} onRemove={() => setFilters((current) => ({ ...current, type: "" }))} />
        ) : null}
        {filters.deliveryStatus ? (
          <FilterChip
            label={`Status: ${filters.deliveryStatus}`}
            onRemove={() => setFilters((current) => ({ ...current, deliveryStatus: "" }))}
          />
        ) : null}
      </FilterBar>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Events"
            columns={eventColumns}
            data={items}
            isLoading={isLoading}
            onRowClick={setSelectedEvent}
            loadingRows={<SkeletonRows columns={eventColumns.length} />}
            emptyState={<EmptyState title="No events yet" description="Events enqueued by the API will appear here." />}
          />

          {hasMore ? (
            <button
              type="button"
              onClick={loadMore}
              disabled={isLoadingMore}
              className="min-h-11 self-start rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {isLoadingMore ? "Loading…" : "Load more"}
            </button>
          ) : null}
        </>
      )}

      <EventDrawer orgId={orgId} event={selectedEvent} onClose={() => setSelectedEvent(null)} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The events list could not be loaded.";
  }
  return "The events list could not be loaded.";
}
