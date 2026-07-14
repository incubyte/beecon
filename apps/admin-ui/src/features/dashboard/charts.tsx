import { Bar, BarChart, CartesianGrid, Legend, LabelList, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import { usePrefersReducedMotion } from "@/lib/motion";
import type { DashboardMetricsSummary } from "@/lib/api-types";

/** CONNECTION_STATUS_ORDER mirrors connections.Status's fixed lifecycle
 * order (metrics.go's own connectionStatuses literal) so the chart's
 * category order never depends on map iteration order. */
const CONNECTION_STATUS_ORDER = ["ACTIVE", "INITIATED", "EXPIRED", "DISCONNECTED"];

const AXIS_TICK_STYLE = { fill: "var(--text-secondary)", fontSize: 12 };
const LABEL_STYLE = { fill: "var(--text)", fontSize: 12 };

export interface ConnectionsByStatusChartProps {
  connectionsByStatus: Record<string, number>;
}

/** ConnectionsByStatusChart is the dashboard's first chart (AC4/AC5): one
 * bar per connection lifecycle status, each labeled on the x-axis by its
 * own status name and annotated with its exact count via LabelList — the
 * status name and the printed count are both text, so the series is never
 * distinguished by color alone even before considering the bars' shared
 * single color. Animation is skipped entirely under prefers-reduced-motion
 * (AC5). */
export function ConnectionsByStatusChart({ connectionsByStatus }: ConnectionsByStatusChartProps) {
  const prefersReducedMotion = usePrefersReducedMotion();
  const data = CONNECTION_STATUS_ORDER.map((status) => ({ status, count: connectionsByStatus[status] ?? 0 }));

  return (
    <div
      className="rounded-lg border border-border bg-surface p-4"
      data-chart-animation={prefersReducedMotion ? "disabled" : "enabled"}
    >
      <h2 className="mb-3 text-sm font-medium text-text">Connections by status</h2>
      <ResponsiveContainer width="100%" height={220}>
        <BarChart data={data} role="img" aria-label="Bar chart of connections grouped by lifecycle status">
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
          {/* interval={0} forces every category tick to render — with only
           * four fixed statuses (or the small, bounded set of event types
           * below) recharts' own overlap-avoidance heuristic otherwise hides
           * most of them, which would also hide their text label from real
           * operators, not just from a test. */}
          <XAxis dataKey="status" tick={AXIS_TICK_STYLE} interval={0} />
          <YAxis allowDecimals={false} tick={AXIS_TICK_STYLE} />
          <Tooltip />
          <Bar dataKey="count" name="Connections" fill="var(--primary)" isAnimationActive={!prefersReducedMotion} radius={[4, 4, 0, 0]}>
            <LabelList dataKey="count" position="top" style={LABEL_STYLE} />
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

export interface DeliveryOutcomesChartProps {
  deliveryOutcomes: DashboardMetricsSummary["deliveryOutcomes"];
}

/** DeliveryOutcomesChart is the dashboard's second chart (AC4/AC5): webhook
 * delivery attempts grouped by event type, one bar per result (success/
 * failure) per group — each series carries its own text name in the Legend
 * (not just a color swatch) and its own LabelList value annotation, so the
 * two results stay distinguishable in grayscale (DESIGN.md §9). */
export function DeliveryOutcomesChart({ deliveryOutcomes }: DeliveryOutcomesChartProps) {
  const prefersReducedMotion = usePrefersReducedMotion();
  const data = groupOutcomesByType(deliveryOutcomes);

  return (
    <div
      className="rounded-lg border border-border bg-surface p-4"
      data-chart-animation={prefersReducedMotion ? "disabled" : "enabled"}
    >
      <h2 className="mb-3 text-sm font-medium text-text">Delivery outcomes by type</h2>
      {data.length === 0 ? (
        <p className="text-sm text-text-muted">No delivery attempts recorded yet.</p>
      ) : (
        <ResponsiveContainer width="100%" height={240}>
          <BarChart data={data} role="img" aria-label="Bar chart of webhook delivery outcomes grouped by event type">
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
            <XAxis dataKey="type" tick={AXIS_TICK_STYLE} interval={0} />
            <YAxis allowDecimals={false} tick={AXIS_TICK_STYLE} />
            <Tooltip />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            <Bar dataKey="success" name="Success" fill="var(--success-solid)" isAnimationActive={!prefersReducedMotion} radius={[4, 4, 0, 0]}>
              <LabelList dataKey="success" position="top" style={LABEL_STYLE} />
            </Bar>
            <Bar dataKey="failure" name="Failure" fill="var(--error-solid)" isAnimationActive={!prefersReducedMotion} radius={[4, 4, 0, 0]}>
              <LabelList dataKey="failure" position="top" style={LABEL_STYLE} />
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      )}
    </div>
  );
}

interface OutcomesByType {
  type: string;
  success: number;
  failure: number;
}

function groupOutcomesByType(outcomes: DashboardMetricsSummary["deliveryOutcomes"]): OutcomesByType[] {
  const byType = new Map<string, OutcomesByType>();
  for (const outcome of outcomes) {
    const entry = byType.get(outcome.type) ?? { type: outcome.type, success: 0, failure: 0 };
    if (outcome.result === "success") {
      entry.success = outcome.count;
    } else {
      entry.failure = outcome.count;
    }
    byType.set(outcome.type, entry);
  }
  return Array.from(byType.values());
}
