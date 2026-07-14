package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestSummaryHandler_WritesTheTypedJSONSummaryShape pins GET
// /api/v1/dashboard/metrics's wire contract (Slice 3, API Shape): a 200 with
// a JSON body carrying exactly the field names the Admin UI's
// DashboardMetricsSummary type expects (connectionsByStatus, outbox with
// pendingDepth/oldestPendingAgeSeconds, deliveryOutcomes with
// type/result/count) — the same values Summary() computed, serialized.
func TestSummaryHandler_WritesTheTypedJSONSummaryShape(t *testing.T) {
	registry := New()
	registry.RegisterConnectionsByStatusGauge(func(ctx context.Context) (map[string]int, error) {
		return map[string]int{"ACTIVE": 1}, nil
	})
	registry.RegisterOutboxGauges(
		func(ctx context.Context) (int, error) { return 2, nil },
		func(ctx context.Context) (time.Duration, error) { return 7 * time.Second, nil },
	)
	registry.RecordDeliveryAttempt("trigger.event", true)

	req := httptest.NewRequest(http.MethodGet, "/dashboard/metrics", nil)
	w := httptest.NewRecorder()
	registry.SummaryHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var body struct {
		ConnectionsByStatus map[string]int `json:"connectionsByStatus"`
		Outbox              struct {
			PendingDepth            int     `json:"pendingDepth"`
			OldestPendingAgeSeconds float64 `json:"oldestPendingAgeSeconds"`
		} `json:"outbox"`
		DeliveryOutcomes []struct {
			Type   string `json:"type"`
			Result string `json:"result"`
			Count  int    `json:"count"`
		} `json:"deliveryOutcomes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}

	if body.ConnectionsByStatus["ACTIVE"] != 1 {
		t.Errorf("connectionsByStatus.ACTIVE = %d, want 1", body.ConnectionsByStatus["ACTIVE"])
	}
	if body.Outbox.PendingDepth != 2 {
		t.Errorf("outbox.pendingDepth = %d, want 2", body.Outbox.PendingDepth)
	}
	if body.Outbox.OldestPendingAgeSeconds != 7 {
		t.Errorf("outbox.oldestPendingAgeSeconds = %v, want 7", body.Outbox.OldestPendingAgeSeconds)
	}

	found := false
	for _, outcome := range body.DeliveryOutcomes {
		if outcome.Type == "trigger.event" && outcome.Result == "success" && outcome.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("deliveryOutcomes = %+v, want an entry for trigger.event/success/1", body.DeliveryOutcomes)
	}
}
