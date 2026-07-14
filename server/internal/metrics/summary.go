package metrics

import (
	dto "github.com/prometheus/client_model/go"
)

// DeliveryOutcome is one (event type, result) pair's cumulative attempt
// count (Slice 3 dashboard): result is "success" or "failure"
// (outcomeSuccess/outcomeFailure).
type DeliveryOutcome struct {
	Type   string
	Result string
	Count  int
}

// OutboxSnapshot is the outbox's current depth and the age of its oldest
// PENDING event, read at request time (Slice 3 dashboard) — the same
// scrape-time query RegisterOutboxGauges already exposes as Prometheus
// gauges, reflected here as plain numbers.
type OutboxSnapshot struct {
	PendingDepth            int
	OldestPendingAgeSeconds float64
}

// Summary is the Admin UI dashboard's headline read (Slice 3, architecture
// doc §3): connections-by-status, outbox depth/oldest-pending-age, and
// webhook delivery outcomes by event type and result. It is built by
// Gather()ing this Registry's own already-registered families rather than
// keeping a second, parallel set of counters — the JSON summary and the
// Prometheus text scrape can never drift apart because they read the exact
// same values.
type Summary struct {
	ConnectionsByStatus map[string]int
	Outbox              OutboxSnapshot
	DeliveryOutcomes    []DeliveryOutcome
}

// Summary gathers this Registry's current metric values into the typed
// shape the dashboard's JSON endpoint renders (Slice 3). It never errors in
// practice (Gather only fails when a registered collector itself
// misbehaves), but the error is surfaced rather than swallowed so a future
// bad collector fails loudly instead of silently reporting a zeroed
// dashboard.
func (r *Registry) Summary() (Summary, error) {
	families, err := r.registry.Gather()
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{ConnectionsByStatus: map[string]int{}}
	for _, family := range families {
		switch family.GetName() {
		case "beecon_connections_by_status":
			applyConnectionsByStatus(&summary, family.GetMetric())
		case "beecon_outbox_pending_depth":
			summary.Outbox.PendingDepth = int(firstGaugeValue(family.GetMetric()))
		case "beecon_outbox_oldest_pending_age_seconds":
			summary.Outbox.OldestPendingAgeSeconds = firstGaugeValue(family.GetMetric())
		case "beecon_delivery_attempts_total":
			summary.DeliveryOutcomes = append(summary.DeliveryOutcomes, deliveryOutcomes(family.GetMetric())...)
		}
	}
	return summary, nil
}

func applyConnectionsByStatus(summary *Summary, metrics []*dto.Metric) {
	for _, metric := range metrics {
		status := labelValue(metric, "status")
		summary.ConnectionsByStatus[status] = int(metric.GetGauge().GetValue())
	}
}

func deliveryOutcomes(metrics []*dto.Metric) []DeliveryOutcome {
	outcomes := make([]DeliveryOutcome, 0, len(metrics))
	for _, metric := range metrics {
		outcomes = append(outcomes, DeliveryOutcome{
			Type:   labelValue(metric, "type"),
			Result: labelValue(metric, "result"),
			Count:  int(metric.GetCounter().GetValue()),
		})
	}
	return outcomes
}

func firstGaugeValue(metrics []*dto.Metric) float64 {
	if len(metrics) == 0 {
		return 0
	}
	return metrics[0].GetGauge().GetValue()
}

func labelValue(metric *dto.Metric, name string) string {
	for _, pair := range metric.GetLabel() {
		if pair.GetName() == name {
			return pair.GetValue()
		}
	}
	return ""
}
