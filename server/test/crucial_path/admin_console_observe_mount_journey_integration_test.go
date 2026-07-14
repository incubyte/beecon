//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, wireErrorEnvelope,
// doJSONRequest, createOrgAndKey (key_rotation_journey_integration_test.go);
// logEntryDTO, logsPageDTO, fetchLogs (tool_execution_journey_integration_test.go);
// outboxEventDTO, outboxEventsPageDTO, webhookEndpointCreatedDTO,
// setWebhookEndpoint, sendTestEvent, listOutboxEvents, redeliverEvent,
// dispatcherLoopName, findOutboxEvent (webhook_channel_journey_integration_test.go)
// — same package). This file tells Slice 3's own "watching the pipes" story
// at the admin-console mount: the same AdminOrgScope bridge Slice 2 proved
// for connections/trigger-instances (admin_console_operate_mount_journey_integration_test.go)
// now also fronts the pre-existing org-key-guarded logs and events handlers,
// reused verbatim under /api/v1/organizations/{orgId}/logs and
// .../events — scoped strictly by the {orgId} in the path, never leaking
// across organizations — while the pre-existing org-key
// /api/v1/logs and /api/v1/events routes (the SDK's own surface) keep
// working exactly as before, and a manual redeliver reachable through the
// admin mount re-queues a FAILED event for another attempt.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/app"
	deliverybun "beecon/internal/delivery/driven/bun"
	"beecon/test/support"
)

// deliverOneTestEventFor wires orgAuth's own webhook endpoint against a
// FakeReceiver that always answers 200, sends one test event, and dispatches
// it once — leaving that organization with exactly one DELIVERED outbox
// event and its one companion webhook_delivery log entry. Returns the
// delivered event's own id.
func deliverOneTestEventFor(t *testing.T, wired *app.Wired, orgAuth string) string {
	t.Helper()
	receiver := support.NewFakeReceiver(t, support.FakeReceiverScript{})
	if status, _ := setWebhookEndpoint(t, wired.Router, orgAuth, receiver.URL); status != http.StatusOK {
		t.Fatalf("set endpoint status = %d, want %d", status, http.StatusOK)
	}
	if status := sendTestEvent(t, wired.Router, orgAuth); status != http.StatusAccepted {
		t.Fatalf("send-test status = %d, want %d", status, http.StatusAccepted)
	}
	if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	status, page := listOutboxEvents(t, wired.Router, orgAuth, "?deliveryStatus=DELIVERED")
	if status != http.StatusOK {
		t.Fatalf("list events status = %d, want %d", status, http.StatusOK)
	}
	if len(page.Items) != 1 {
		t.Fatalf("delivered events = %+v, want exactly 1", page.Items)
	}
	return page.Items[0].ID
}

// setOutboxEventStatus writes an outbox_events row's status directly,
// mirroring setConnectionStatus's own direct-row-flip precedent
// (trigger_instances_journey_integration_test.go) — used here to put a
// DELIVERED event back into FAILED so this file can exercise a manual
// redeliver without re-running a real multi-hour retry schedule.
func setOutboxEventStatus(t *testing.T, wired *app.Wired, eventID, status string) {
	t.Helper()
	_, err := wired.DB.NewUpdate().
		Model((*deliverybun.EventRow)(nil)).
		Set("status = ?", status).
		Where("id = ?", eventID).
		Exec(context.Background())
	if err != nil {
		t.Fatalf("set outbox event status: %v", err)
	}
}

func listLogsUnderAdminMount(t *testing.T, wired *app.Wired, orgID, authHeader string) (int, logsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/logs", authHeader, "")
	var page logsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode logs page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func listEventsUnderAdminMount(t *testing.T, wired *app.Wired, orgID, authHeader string) (int, outboxEventsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+orgID+"/events", authHeader, "")
	var page outboxEventsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode events page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func redeliverEventUnderAdminMount(t *testing.T, wired *app.Wired, orgID, eventID, authHeader string) int {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgID+"/events/"+eventID+"/redeliver", authHeader, "")
	return w.Code
}

func containsLogID(entries []logEntryDTO, id string) bool {
	for _, entry := range entries {
		if entry.ID == id {
			return true
		}
	}
	return false
}

// TestAdminConsoleLogsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact
// covers Slice 3's AC1/AC6 at the wire level: an operator holding only the
// admin key lists exactly one organization's log entries under
// /api/v1/organizations/{orgId}/logs, scoped by the path org id with no
// cross-org bleed, and the pre-existing org-key /api/v1/logs route is
// unaffected by the second mount.
func TestAdminConsoleLogsMount_ScopesToThePathOrgIDAndLeavesTheSDKRouteIntact(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	orgA, keyA := createOrgAndKey(t, wired.Router, adminAuth, "Acme")
	orgAAuth := "Bearer " + keyA.Key
	orgB, keyB := createOrgAndKey(t, wired.Router, adminAuth, "Globex")
	orgBAuth := "Bearer " + keyB.Key

	eventAID := deliverOneTestEventFor(t, wired, orgAAuth)
	_ = deliverOneTestEventFor(t, wired, orgBAuth)

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := listLogsUnderAdminMount(t, wired, orgA.ID, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		status, _ := listLogsUnderAdminMount(t, wired, orgA.ID, orgAAuth)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", status, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path returns only org A's log entries", func(t *testing.T) {
		status, page := listLogsUnderAdminMount(t, wired, orgA.ID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		for _, entry := range page.Entries {
			if entry.OrgID != orgA.ID {
				t.Errorf("entry %q organizationId = %q, want %q — org B's log leaked into org A's page", entry.ID, entry.OrgID, orgA.ID)
			}
		}
		if len(page.Entries) == 0 {
			t.Fatal("org A's log page is empty, want at least the webhook_delivery entry from deliverOneTestEventFor")
		}
	})

	t.Run("the same admin key against org B's path returns only org B's log entries (path is authoritative)", func(t *testing.T) {
		status, page := listLogsUnderAdminMount(t, wired, orgB.ID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if containsLogID(page.Entries, eventAID) {
			t.Errorf("org B's log page unexpectedly references org A's event %q", eventAID)
		}
		for _, entry := range page.Entries {
			if entry.OrgID != orgB.ID {
				t.Errorf("entry %q organizationId = %q, want %q — org A's log leaked into org B's page", entry.ID, entry.OrgID, orgB.ID)
			}
		}
	})

	t.Run("regression: the pre-existing SDK-facing org-key route still lists org A's log entries", func(t *testing.T) {
		page := fetchLogs(t, wired, orgAAuth, "")
		if len(page.Entries) == 0 {
			t.Fatal("/api/v1/logs entries are empty, want org A's webhook_delivery entry — the admin mount must not have disturbed the original org-key route")
		}
		for _, entry := range page.Entries {
			if entry.OrgID != orgA.ID {
				t.Errorf("/api/v1/logs entry %q organizationId = %q, want %q", entry.ID, entry.OrgID, orgA.ID)
			}
		}
	})
}

// TestAdminConsoleEventsMount_ScopesToThePathOrgIDRedeliverWorksAndLeavesTheSDKRouteIntact
// covers Slice 3's AC2/AC3 at the wire level: listing and redelivering
// events through /api/v1/organizations/{orgId}/events is scoped by the path
// org id with no cross-org bleed, redeliver through the admin mount is the
// same handler reused verbatim (it re-queues a FAILED event and a
// subsequent dispatch delivers it), and the pre-existing org-key
// /api/v1/events route is unaffected.
func TestAdminConsoleEventsMount_ScopesToThePathOrgIDRedeliverWorksAndLeavesTheSDKRouteIntact(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	orgA, keyA := createOrgAndKey(t, wired.Router, adminAuth, "Acme")
	orgAAuth := "Bearer " + keyA.Key
	orgB, keyB := createOrgAndKey(t, wired.Router, adminAuth, "Globex")
	orgBAuth := "Bearer " + keyB.Key

	eventAID := deliverOneTestEventFor(t, wired, orgAAuth)
	eventBID := deliverOneTestEventFor(t, wired, orgBAuth)

	t.Run("no admin key is unauthorized", func(t *testing.T) {
		status, _ := listEventsUnderAdminMount(t, wired, orgA.ID, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", status, http.StatusUnauthorized)
		}
	})

	t.Run("org A's own API key does not satisfy the admin console mount", func(t *testing.T) {
		status, _ := listEventsUnderAdminMount(t, wired, orgA.ID, orgAAuth)
		if status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the console mount is admin-key-only, not org-key", status, http.StatusUnauthorized)
		}
	})

	t.Run("the admin key against org A's path returns only org A's event", func(t *testing.T) {
		status, page := listEventsUnderAdminMount(t, wired, orgA.ID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if findOutboxEvent(page.Items, eventAID) == nil {
			t.Errorf("org A's page = %+v, want it to include event %q", page.Items, eventAID)
		}
		if findOutboxEvent(page.Items, eventBID) != nil {
			t.Errorf("org A's page = %+v leaked org B's event %q", page.Items, eventBID)
		}
	})

	t.Run("the same admin key against org B's path returns only org B's event (path is authoritative)", func(t *testing.T) {
		status, page := listEventsUnderAdminMount(t, wired, orgB.ID, adminAuth)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if findOutboxEvent(page.Items, eventBID) == nil {
			t.Errorf("org B's page = %+v, want it to include event %q", page.Items, eventBID)
		}
		if findOutboxEvent(page.Items, eventAID) != nil {
			t.Errorf("org B's page = %+v leaked org A's event %q", page.Items, eventAID)
		}
	})

	t.Run("redeliver through the admin mount transitions a FAILED event back to DELIVERED", func(t *testing.T) {
		setOutboxEventStatus(t, wired, eventAID, "FAILED")

		status := redeliverEventUnderAdminMount(t, wired, orgA.ID, eventAID, adminAuth)
		if status != http.StatusAccepted {
			t.Fatalf("redeliver status = %d, want %d", status, http.StatusAccepted)
		}
		if err := wired.Workers.RunOnce(context.Background(), dispatcherLoopName); err != nil {
			t.Fatalf("DispatchOnce: %v", err)
		}

		listStatus, page := listEventsUnderAdminMount(t, wired, orgA.ID, adminAuth)
		if listStatus != http.StatusOK {
			t.Fatalf("list status = %d, want %d", listStatus, http.StatusOK)
		}
		found := findOutboxEvent(page.Items, eventAID)
		if found == nil {
			t.Fatalf("event %q missing from org A's page after redeliver", eventAID)
		}
		if found.DeliveryStatus != "DELIVERED" {
			t.Errorf("deliveryStatus = %q, want %q after redeliver + dispatch", found.DeliveryStatus, "DELIVERED")
		}
	})

	t.Run("regression: the pre-existing SDK-facing org-key route still lists org B's event", func(t *testing.T) {
		status, page := listOutboxEvents(t, wired.Router, orgBAuth, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if findOutboxEvent(page.Items, eventBID) == nil {
			t.Fatalf("/api/v1/events items = %+v, want them to include %q — the admin mount must not have disturbed the original org-key route", page.Items, eventBID)
		}
	})
}
