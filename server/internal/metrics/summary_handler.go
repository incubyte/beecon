package metrics

import (
	"net/http"

	"beecon/internal/httpx"
)

// summaryDTO is GET /api/v1/dashboard/metrics's response shape (Slice 3 API
// Shape): the dashboard's headline figures, sourced from the same
// Prometheus registry /metrics scrapes in text format.
type summaryDTO struct {
	ConnectionsByStatus map[string]int    `json:"connectionsByStatus"`
	Outbox              outboxDTO         `json:"outbox"`
	DeliveryOutcomes    []deliveryOutcome `json:"deliveryOutcomes"`
}

type outboxDTO struct {
	PendingDepth            int     `json:"pendingDepth"`
	OldestPendingAgeSeconds float64 `json:"oldestPendingAgeSeconds"`
}

type deliveryOutcome struct {
	Type   string `json:"type"`
	Result string `json:"result"`
	Count  int    `json:"count"`
}

func toSummaryDTO(summary Summary) summaryDTO {
	outcomes := make([]deliveryOutcome, 0, len(summary.DeliveryOutcomes))
	for _, outcome := range summary.DeliveryOutcomes {
		outcomes = append(outcomes, deliveryOutcome{Type: outcome.Type, Result: outcome.Result, Count: outcome.Count})
	}
	return summaryDTO{
		ConnectionsByStatus: summary.ConnectionsByStatus,
		Outbox: outboxDTO{
			PendingDepth:            summary.Outbox.PendingDepth,
			OldestPendingAgeSeconds: summary.Outbox.OldestPendingAgeSeconds,
		},
		DeliveryOutcomes: outcomes,
	}
}

// SummaryHandler serves GET /api/v1/dashboard/metrics (Slice 3, admin-key
// guarded, installation-wide like GET /metrics itself): a small typed JSON
// read over the same registry the Prometheus text endpoint exposes, chosen
// over having the SPA parse Prometheus text client-side — a typed console
// UI gets typed numbers, and there is exactly one source of truth (Gather)
// behind both endpoints.
func (r *Registry) SummaryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		summary, err := r.Summary()
		if err != nil {
			httpx.WriteDomainError(w, httpx.New(http.StatusInternalServerError, "internal_error", "failed to read metrics"))
			return
		}
		httpx.WriteJSON(w, http.StatusOK, toSummaryDTO(summary))
	}
}
