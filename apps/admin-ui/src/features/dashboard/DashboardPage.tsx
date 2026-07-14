import { Clock, Layers, Radio, Webhook } from "lucide-react";

import { ErrorCard } from "@/components/ui/ErrorCard";
import { MetricTile } from "@/components/ui/MetricTile";
import { ApiError } from "@/lib/api-client";
import { formatDurationSeconds } from "@/lib/format";

import { useDashboardMetrics } from "./api";
import { ConnectionsByStatusChart, DeliveryOutcomesChart } from "./charts";

/**
 * DashboardPage is Slice 3's default post-login landing (architecture doc
 * §8 Slice 3 scope): a MetricTile row of headline operability figures plus
 * two charts, sourced from GET /dashboard/metrics (this slice's "metrics
 * read path" decision — a typed JSON summary over the same Prometheus
 * registry /metrics exposes in text format, rather than the SPA parsing
 * Prometheus text client-side). Installation-wide — no org selection is
 * required to see it (AC4).
 *
 * The two charts present the registry's current cumulative snapshot broken
 * out by category (connection status; event type × result) rather than a
 * true historical time series: the backend keeps live gauges/counters, not
 * a retained series of past samples, and adding one is out of this slice's
 * scope (YAGNI — no AC asks for historical trending). Both still satisfy
 * AC5's "distinguish series by more than color" and reduced-motion
 * requirements.
 */
export function DashboardPage() {
  const { data: metrics, isLoading, isError, error, refetch } = useDashboardMetrics();

  if (isError) {
    return <ErrorCard message={errorMessage(error)} onRetry={refetch} />;
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Dashboard</h1>
        <p className="text-sm text-text-secondary">Operability metrics across every organization in this installation.</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetricTile
          icon={Layers}
          label="Active connections"
          value={isLoading ? "…" : (metrics?.connectionsByStatus.ACTIVE ?? 0)}
        />
        <MetricTile icon={Webhook} label="Outbox pending" value={isLoading ? "…" : (metrics?.outbox.pendingDepth ?? 0)} />
        <MetricTile
          icon={Clock}
          label="Oldest pending event"
          value={isLoading ? "…" : formatDurationSeconds(metrics?.outbox.oldestPendingAgeSeconds ?? 0)}
        />
        <MetricTile
          icon={Radio}
          label="Delivery success rate"
          value={isLoading ? "…" : formatSuccessRate(metrics?.deliveryOutcomes ?? [])}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ConnectionsByStatusChart connectionsByStatus={metrics?.connectionsByStatus ?? {}} />
        <DeliveryOutcomesChart deliveryOutcomes={metrics?.deliveryOutcomes ?? []} />
      </div>
    </div>
  );
}

function formatSuccessRate(deliveryOutcomes: { result: string; count: number }[]): string {
  const successes = sumByResult(deliveryOutcomes, "success");
  const failures = sumByResult(deliveryOutcomes, "failure");
  const total = successes + failures;
  if (total === 0) {
    return "—";
  }
  return `${Math.round((successes / total) * 100)}%`;
}

function sumByResult(deliveryOutcomes: { result: string; count: number }[], result: string): number {
  return deliveryOutcomes.filter((outcome) => outcome.result === result).reduce((sum, outcome) => sum + outcome.count, 0);
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The dashboard metrics could not be loaded.";
  }
  return "The dashboard metrics could not be loaded.";
}
