package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"beecon/internal/config"
	"beecon/internal/db"
	"beecon/internal/metrics"
)

const dashboardMetricsTestAdminKey = "test-admin-key"

// newDashboardMetricsRouter builds the real router with a real
// metrics.Registry's SummaryHandler mounted exactly as app/router.go wires
// it — GET /api/v1/dashboard/metrics behind AdminAuth, installation-wide —
// while every other handler stays nil, mirroring
// newOrganizationsListRouter's own convention
// (organizations_list_route_admin_guard_test.go).
func newDashboardMetricsRouter(t *testing.T) http.Handler {
	t.Helper()
	database, err := db.New(config.DriverSQLite, "file:dashboard_metrics_route_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	registry := metrics.New()
	registry.RegisterConnectionsByStatusGauge(func(ctx context.Context) (map[string]int, error) {
		return map[string]int{"ACTIVE": 4, "INITIATED": 0, "EXPIRED": 0, "DISCONNECTED": 1}, nil
	})
	registry.RegisterOutboxGauges(
		func(ctx context.Context) (int, error) { return 3, nil },
		func(ctx context.Context) (time.Duration, error) { return 90 * time.Second, nil },
	)
	registry.RecordDeliveryAttempt("trigger.event", true)
	registry.RecordDeliveryAttempt("trigger.event", false)

	cfg := &config.Config{AdminAPIKey: dashboardMetricsTestAdminKey}
	noOperatorsYet := func(context.Context) (bool, error) { return false, nil }
	return buildRouter(
		cfg, database,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, // organizationsHandler..deliveryHandler
		nil,                       // operatorHandler
		nil,                       // metricsHandler
		registry.SummaryHandler(), // dashboardMetricsHandler
		nil, nil, nil,             // verifyOrgKey, verifyUserToken, verifySession
		noOperatorsYet, // operatorsExist
		nil,            // logger
	)
}

type dashboardMetricsBody struct {
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

func doDashboardMetricsRequest(router http.Handler, path, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestDashboardMetricsRoute_RejectsARequestWithNoAdminKey(t *testing.T) {
	router := newDashboardMetricsRouter(t)

	w := doDashboardMetricsRequest(router, "/api/v1/dashboard/metrics", "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestDashboardMetricsRoute_RejectsAWrongAdminKey(t *testing.T) {
	router := newDashboardMetricsRouter(t)

	w := doDashboardMetricsRequest(router, "/api/v1/dashboard/metrics", "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestDashboardMetricsRoute_IsNotNestedUnderAnOrganizationPath pins that the
// dashboard summary is installation-wide (architecture doc §3): unlike
// connections/trigger-instances/logs/events, it has no
// /organizations/{orgId}/... form at all — requesting one 404s rather than
// silently scoping to that org.
func TestDashboardMetricsRoute_IsNotNestedUnderAnOrganizationPath(t *testing.T) {
	router := newDashboardMetricsRouter(t)

	w := doDashboardMetricsRequest(router, "/api/v1/organizations/org_whatever/dashboard/metrics", "Bearer "+dashboardMetricsTestAdminKey)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d — the dashboard summary must not have an org-scoped path form", w.Code, http.StatusNotFound)
	}
}

// TestDashboardMetricsRoute_Returns200WithTheTypedSummaryShapeReflectingTheRegistry
// pins the endpoint's happy path: behind the correct admin key, at the
// installation-wide path (no orgId anywhere), it returns exactly the figures
// the wired metrics.Registry currently holds.
func TestDashboardMetricsRoute_Returns200WithTheTypedSummaryShapeReflectingTheRegistry(t *testing.T) {
	router := newDashboardMetricsRouter(t)

	w := doDashboardMetricsRequest(router, "/api/v1/dashboard/metrics", "Bearer "+dashboardMetricsTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body dashboardMetricsBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if body.ConnectionsByStatus["ACTIVE"] != 4 {
		t.Errorf("connectionsByStatus.ACTIVE = %d, want 4", body.ConnectionsByStatus["ACTIVE"])
	}
	if body.ConnectionsByStatus["DISCONNECTED"] != 1 {
		t.Errorf("connectionsByStatus.DISCONNECTED = %d, want 1", body.ConnectionsByStatus["DISCONNECTED"])
	}
	if body.Outbox.PendingDepth != 3 {
		t.Errorf("outbox.pendingDepth = %d, want 3", body.Outbox.PendingDepth)
	}
	if body.Outbox.OldestPendingAgeSeconds != 90 {
		t.Errorf("outbox.oldestPendingAgeSeconds = %v, want 90", body.Outbox.OldestPendingAgeSeconds)
	}
	foundSuccess, foundFailure := false, false
	for _, outcome := range body.DeliveryOutcomes {
		if outcome.Type == "trigger.event" && outcome.Result == "success" && outcome.Count == 1 {
			foundSuccess = true
		}
		if outcome.Type == "trigger.event" && outcome.Result == "failure" && outcome.Count == 1 {
			foundFailure = true
		}
	}
	if !foundSuccess {
		t.Errorf("deliveryOutcomes = %+v, want a trigger.event/success/1 entry", body.DeliveryOutcomes)
	}
	if !foundFailure {
		t.Errorf("deliveryOutcomes = %+v, want a trigger.event/failure/1 entry", body.DeliveryOutcomes)
	}
}
