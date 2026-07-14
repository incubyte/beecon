//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, integrationSummaryDTO,
// wireErrorEnvelope, doJSONRequest (organizations_journey_integration_test.go);
// setUpOrgWithConnection (admin_console_operate_mount_journey_integration_test.go)
// — same package). This file tells Slice 3's dashboard story end to end
// against the real composition root: GET /api/v1/dashboard/metrics is
// admin-guarded and installation-wide (no /organizations/{orgId}/... form
// exists for it, unlike every other console read this phase adds), and its
// figures are the same metrics.Registry /metrics itself scrapes — driving
// one real connection and one real webhook delivery makes the JSON summary
// move, combined across every organization in the installation rather than
// scoped to whichever org happened to make the request (there is no org in
// the request at all).
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"beecon/test/support"
)

type dashboardOutboxDTO struct {
	PendingDepth            int     `json:"pendingDepth"`
	OldestPendingAgeSeconds float64 `json:"oldestPendingAgeSeconds"`
}

type dashboardDeliveryOutcomeDTO struct {
	Type   string `json:"type"`
	Result string `json:"result"`
	Count  int    `json:"count"`
}

type dashboardMetricsSummaryDTO struct {
	ConnectionsByStatus map[string]int                `json:"connectionsByStatus"`
	Outbox              dashboardOutboxDTO            `json:"outbox"`
	DeliveryOutcomes    []dashboardDeliveryOutcomeDTO `json:"deliveryOutcomes"`
}

func fetchDashboardMetrics(t *testing.T, router http.Handler, authHeader string) (int, dashboardMetricsSummaryDTO) {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/dashboard/metrics", authHeader, "")
	var summary dashboardMetricsSummaryDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
			t.Fatalf("decode dashboard metrics: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, summary
}

func deliveryOutcomeCount(summary dashboardMetricsSummaryDTO, eventType, result string) (int, bool) {
	for _, outcome := range summary.DeliveryOutcomes {
		if outcome.Type == eventType && outcome.Result == result {
			return outcome.Count, true
		}
	}
	return 0, false
}

// TestDashboardMetricsJourney_AdminGuardedAndInstallationWide covers the
// access-control half of Slice 3's dashboard AC: no key and a wrong key are
// both unauthorized, an org's own API key does not satisfy it either (the
// dashboard is an admin-console surface, not part of the org-key SDK
// surface), and — unlike connections/logs/events — there is no
// /organizations/{orgId}/dashboard/metrics path at all.
func TestDashboardMetricsJourney_AdminGuardedAndInstallationWide(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Acme")
	orgAuth := "Bearer " + issued.Key

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := fetchDashboardMetrics(t, wired.Router, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("a wrong admin key is unauthorized", func(t *testing.T) {
		status, _ := fetchDashboardMetrics(t, wired.Router, "Bearer wrong-key")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("an org's own API key does not satisfy the dashboard endpoint", func(t *testing.T) {
		status, _ := fetchDashboardMetrics(t, wired.Router, orgAuth)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the dashboard is admin-key-only", status, http.StatusUnauthorized)
		}
	})

	t.Run("there is no org-scoped path form for the dashboard summary", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+org.ID+"/dashboard/metrics", adminAuth, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("the admin key at the installation-wide path succeeds", func(t *testing.T) {
		status, _ := fetchDashboardMetrics(t, wired.Router, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
	})
}

// TestDashboardMetricsJourney_ReflectsRealTrafficCombinedAcrossEveryOrganization
// drives one real connection in each of two organizations and one real
// webhook delivery in one of them, then asserts the installation-wide
// summary reflects BOTH organizations' contributions added together —
// proving the endpoint is a genuine installation-wide aggregate (the
// request itself names no organization) rather than an accidental
// single-org view.
func TestDashboardMetricsJourney_ReflectsRealTrafficCombinedAcrossEveryOrganization(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	var integration integrationSummaryDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"client-id","clientSecret":"client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}
	const redirectURI = "https://consumer.example.com/callback"

	before, _ := fetchDashboardMetrics(t, wired.Router, adminAuth)
	if before != http.StatusOK {
		t.Fatalf("baseline status = %d, want %d", before, http.StatusOK)
	}
	_, baseline := fetchDashboardMetrics(t, wired.Router, adminAuth)
	baselineInitiated := baseline.ConnectionsByStatus["INITIATED"]

	// One INITIATED connection in each of two distinct organizations.
	_, orgAAuth, _ := setUpOrgWithConnection(t, wired, adminAuth, "Acme", integration.ID, redirectURI)
	_, _, _ = setUpOrgWithConnection(t, wired, adminAuth, "Globex", integration.ID, redirectURI)

	// One real, delivered webhook event in org A only.
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, orgAAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	if status := sendTestEvent(t, wired.Router, orgAAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}

	t.Run("before dispatch, the outbox depth reflects the one still-pending event", func(t *testing.T) {
		status, summary := fetchDashboardMetrics(t, wired.Router, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if summary.Outbox.PendingDepth < 1 {
			t.Errorf("outbox.pendingDepth = %d, want >= 1", summary.Outbox.PendingDepth)
		}
	})

	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}

	t.Run("connections-by-status counts BOTH organizations' new connections combined, even though the request names no org", func(t *testing.T) {
		status, summary := fetchDashboardMetrics(t, wired.Router, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		gotInitiated := summary.ConnectionsByStatus["INITIATED"]
		if gotInitiated < baselineInitiated+2 {
			t.Errorf("connectionsByStatus.INITIATED = %d, want >= %d (baseline %d + one connection per organization)", gotInitiated, baselineInitiated+2, baselineInitiated)
		}
	})

	t.Run("after dispatch, the outbox depth drops and the delivery outcome counts the success", func(t *testing.T) {
		status, summary := fetchDashboardMetrics(t, wired.Router, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if summary.Outbox.PendingDepth != 0 {
			t.Errorf("outbox.pendingDepth = %d, want 0 once the only pending event has been delivered", summary.Outbox.PendingDepth)
		}
		count, found := deliveryOutcomeCount(summary, "webhook.test", "success")
		if !found || count < 1 {
			t.Errorf("deliveryOutcomes = %+v, want a webhook.test/success entry with count >= 1", summary.DeliveryOutcomes)
		}
	})
}
