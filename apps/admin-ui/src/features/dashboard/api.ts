import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { DashboardMetricsSummary } from "@/lib/api-types";

/** useDashboardMetrics reads the operability dashboard's headline figures
 * (AC4) via GET /dashboard/metrics: admin-guarded and installation-wide
 * (no org in the path or the query key) — the same metrics.Registry that
 * backs the Prometheus text endpoint, read here as typed JSON. */
export function useDashboardMetrics() {
  return useQuery({
    queryKey: queryKeys.dashboard.metrics(),
    queryFn: () => apiClient.get<DashboardMetricsSummary>("/dashboard/metrics"),
  });
}
