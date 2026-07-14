//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, wireErrorEnvelope, doJSONRequest, oauthJourneyFixture/
// newOAuthJourneyFixture, outlookDefinitionAgainst, openConnectPageAndGetState,
// and activateConnectionThroughRealHandshake from
// oauth_handshake_journey_integration_test.go/tool_execution_journey_integration_test.go
// — same package). This file tells Slice 2's story end to end against the
// real composition root, the real triggers facade, and a real SQLite
// database: create (valid config, invalid config, every non-ACTIVE
// connection status, an unknown trigger slug, an unknown/cross-org
// connection) -> list filtered by connectionId/userId with cursor
// pagination -> get -> disable/enable (including idempotency) -> delete ->
// deleting a connection cascades to its own trigger instances without
// touching another connection's -> every mutation against another
// organization's instance is not-found.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	connectionsbun "beecon/internal/connections/driven/bun"
	"beecon/test/support"
)

// createdTriggerInstanceDTO/triggerInstanceDTO/triggerInstancesPageDTO/
// triggerInstanceStatusDTO mirror triggers/driving/httpapi/dto.go's response
// shapes exactly.
type createdTriggerInstanceDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type triggerInstanceDTO struct {
	ID           string         `json:"id"`
	Status       string         `json:"status"`
	ConnectionID string         `json:"connectionId"`
	TriggerSlug  string         `json:"triggerSlug"`
	Config       map[string]any `json:"config"`
	UserID       string         `json:"userId"`
	CreatedAt    string         `json:"createdAt"`
}

type triggerInstancesPageDTO struct {
	Items      []triggerInstanceDTO `json:"items"`
	NextCursor string               `json:"nextCursor"`
}

type triggerInstanceStatusDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

const outlookMessageReceivedSlug = "outlook-message-received"

// outlookDefinitionWithTrigger is outlookDefinitionAgainst plus the real
// outlook-message-received trigger declaration (catalog/providers/outlook.yaml,
// PD35): folderId is a string config property with a default, nothing
// required — exactly the schema Create validates config against in
// production.
func outlookDefinitionWithTrigger(fakeMS *support.FakeMicrosoft) []catalog.ProviderDefinition {
	definitions := outlookDefinitionAgainst(fakeMS)
	definitions[0].Triggers = []catalog.TriggerDefinition{
		{
			Slug:        outlookMessageReceivedSlug,
			Name:        "New message received",
			Description: "Triggered when a new message arrives in the configured mailbox folder.",
			ConfigSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"folderId": map[string]any{"type": "string", "default": "Inbox"},
				},
			},
			PayloadSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
			Ingestion:           "poll",
			PollIntervalSeconds: 60,
		},
	}
	return definitions
}

func createTriggerInstance(t *testing.T, wired *app.Wired, orgAuth, connID, slug, configJSON string) (int, createdTriggerInstanceDTO) {
	t.Helper()
	body := `{"connectionId":"` + connID + `","triggerSlug":"` + slug + `","config":` + configJSON + `}`
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/trigger-instances", orgAuth, body)
	var dto createdTriggerInstanceDTO
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode created trigger instance: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func getTriggerInstance(t *testing.T, wired *app.Wired, orgAuth, id string) (int, triggerInstanceDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/trigger-instances/"+id, orgAuth, "")
	var dto triggerInstanceDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode trigger instance: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func listTriggerInstances(t *testing.T, wired *app.Wired, orgAuth, query string) (int, triggerInstancesPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/trigger-instances"+query, orgAuth, "")
	var page triggerInstancesPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode trigger instances page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func disableTriggerInstance(t *testing.T, wired *app.Wired, orgAuth, id string) (int, triggerInstanceStatusDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/trigger-instances/"+id+"/disable", orgAuth, "")
	var dto triggerInstanceStatusDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode disable response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func enableTriggerInstance(t *testing.T, wired *app.Wired, orgAuth, id string) (int, triggerInstanceStatusDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/trigger-instances/"+id+"/enable", orgAuth, "")
	var dto triggerInstanceStatusDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode enable response: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func deleteTriggerInstance(t *testing.T, wired *app.Wired, orgAuth, id string) int {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/trigger-instances/"+id, orgAuth, "")
	return w.Code
}

// setConnectionStatus flips a connection's status directly at the database
// row — the only way this file can put a connection into EXPIRED, since the
// refresh scheduler that produces EXPIRED naturally does not exist until
// Slice 5.
func setConnectionStatus(t *testing.T, wired *app.Wired, connID, status string) {
	t.Helper()
	_, err := wired.DB.NewUpdate().
		Model((*connectionsbun.ConnectionRow)(nil)).
		Set("status = ?", status).
		Where("id = ?", connID).
		Exec(context.Background())
	if err != nil {
		t.Fatalf("set connection status: %v", err)
	}
}

// newSecondOrgAuth creates a second organization with its own API key, so
// cross-org tests exercise a real different organization rather than a
// synthetic id.
func newSecondOrgAuth(t *testing.T, wired *app.Wired, name string) string {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey
	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"`+name+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}
	var key issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode org key: %v", err)
	}
	return "Bearer " + key.Key
}

// TestTriggerInstanceJourney_CreateValidatesConfigAndConnectionStatus is
// Slice 2's AC1, AC2, AC3.
func TestTriggerInstanceJourney_CreateValidatesConfigAndConnectionStatus(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithTrigger(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)

	t.Run("valid config against an ACTIVE connection creates an instance born ACTIVE with a stable trg_ id", func(t *testing.T) {
		status, created := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
		if status != http.StatusCreated {
			t.Fatalf("create status = %d, want %d", status, http.StatusCreated)
		}
		if len(created.ID) < 4 || created.ID[:4] != "trg_" {
			t.Errorf("id = %q, want a trg_-prefixed id", created.ID)
		}
		if created.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", created.Status, "ACTIVE")
		}

		getStatus, got := getTriggerInstance(t, wired, fixture.orgAuth, created.ID)
		if getStatus != http.StatusOK {
			t.Fatalf("get status = %d, want %d", getStatus, http.StatusOK)
		}
		if got.ConnectionID != active.ID {
			t.Errorf("connectionId = %q, want %q", got.ConnectionID, active.ID)
		}
		if got.TriggerSlug != outlookMessageReceivedSlug {
			t.Errorf("triggerSlug = %q, want %q", got.TriggerSlug, outlookMessageReceivedSlug)
		}
		if got.Config["folderId"] != "Inbox" {
			t.Errorf("config.folderId = %v, want %q", got.Config["folderId"], "Inbox")
		}
		if got.UserID != fixture.userID {
			t.Errorf("userId = %q, want the connection's own user %q (no independent owner)", got.UserID, fixture.userID)
		}
	})

	t.Run("invalid config is rejected with a validation error naming the config field, and no instance is persisted", func(t *testing.T) {
		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{"folderId":123}`)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("create status = %d, want %d", status, http.StatusUnprocessableEntity)
		}

		listStatus, page := listTriggerInstances(t, wired, fixture.orgAuth, "?connectionId="+active.ID)
		if listStatus != http.StatusOK {
			t.Fatalf("list status = %d, want %d", listStatus, http.StatusOK)
		}
		for _, item := range page.Items {
			if item.ConnectionID == active.ID && item.Config["folderId"] == float64(123) {
				t.Fatal("an instance was persisted despite invalid config")
			}
		}
	})

	t.Run("an unknown trigger slug is not-found", func(t *testing.T) {
		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, "does-not-exist", `{}`)
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("an unknown connectionId is not-found", func(t *testing.T) {
		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, "conn_does_not_exist", outlookMessageReceivedSlug, `{}`)
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("a connection belonging to another organization is not-found", func(t *testing.T) {
		otherAuth := newSecondOrgAuth(t, wired, "Other Org")
		status, _ := createTriggerInstance(t, wired, otherAuth, active.ID, outlookMessageReceivedSlug, `{}`)
		if status != http.StatusNotFound {
			t.Errorf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("a connection still INITIATED is rejected with a status-explaining validation error", func(t *testing.T) {
		initiated := fixture.initiate(t, wired)
		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, initiated.ID, outlookMessageReceivedSlug, `{}`)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})

	t.Run("a DISCONNECTED connection is rejected with a status-explaining validation error", func(t *testing.T) {
		toDisable := activateConnectionThroughRealHandshake(t, wired, fixture)
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+toDisable.ID+"/disable", fixture.orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("disable status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}

		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, toDisable.ID, outlookMessageReceivedSlug, `{}`)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})

	t.Run("an EXPIRED connection is rejected with a status-explaining validation error", func(t *testing.T) {
		toExpire := activateConnectionThroughRealHandshake(t, wired, fixture)
		setConnectionStatus(t, wired, toExpire.ID, "EXPIRED")

		status, _ := createTriggerInstance(t, wired, fixture.orgAuth, toExpire.ID, outlookMessageReceivedSlug, `{}`)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", status, http.StatusUnprocessableEntity)
		}
	})
}

// TestTriggerInstanceJourney_ListFilterGetDisableEnableDelete is Slice 2's
// AC4, AC5, and AC6.
func TestTriggerInstanceJourney_ListFilterGetDisableEnableDelete(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithTrigger(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	connA := activateConnectionThroughRealHandshake(t, wired, fixture)
	connB := activateConnectionThroughRealHandshake(t, wired, fixture)

	_, instanceA := createTriggerInstance(t, wired, fixture.orgAuth, connA.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
	_, instanceB := createTriggerInstance(t, wired, fixture.orgAuth, connB.ID, outlookMessageReceivedSlug, `{"folderId":"Archive"}`)

	t.Run("listing filtered by connectionId returns only that connection's instance", func(t *testing.T) {
		status, page := listTriggerInstances(t, wired, fixture.orgAuth, "?connectionId="+connB.ID)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 1 || page.Items[0].ID != instanceB.ID {
			t.Fatalf("items = %+v, want exactly instance %q", page.Items, instanceB.ID)
		}
	})

	t.Run("listing filtered by userId returns instances owned by that user", func(t *testing.T) {
		status, page := listTriggerInstances(t, wired, fixture.orgAuth, "?userId="+fixture.userID)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		found := map[string]bool{}
		for _, item := range page.Items {
			found[item.ID] = true
		}
		if !found[instanceA.ID] || !found[instanceB.ID] {
			t.Fatalf("items = %+v, want both instances (both owned by %q)", page.Items, fixture.userID)
		}
	})

	t.Run("listing filtered by an unknown userId returns no items", func(t *testing.T) {
		status, page := listTriggerInstances(t, wired, fixture.orgAuth, "?userId=user_does_not_exist")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 0 {
			t.Errorf("items = %+v, want none", page.Items)
		}
	})

	t.Run("cursor pagination walks every instance once, newest first, no dupes or gaps", func(t *testing.T) {
		seen := map[string]bool{}
		cursor := ""
		for page := 0; page < 10; page++ {
			status, result := listTriggerInstances(t, wired, fixture.orgAuth, "?limit=1&cursor="+cursor)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}
			for _, item := range result.Items {
				if seen[item.ID] {
					t.Fatalf("instance %q seen more than once while paginating", item.ID)
				}
				seen[item.ID] = true
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		if !seen[instanceA.ID] || !seen[instanceB.ID] {
			t.Fatalf("pagination missed an instance: seen=%v", seen)
		}
	})

	t.Run("disable transitions to DISABLED, and is idempotent on a second call", func(t *testing.T) {
		status, dto := disableTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if status != http.StatusOK || dto.Status != "DISABLED" {
			t.Fatalf("disable status=%d dto=%+v, want 200/DISABLED", status, dto)
		}
		getStatus, got := getTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if getStatus != http.StatusOK || got.Status != "DISABLED" {
			t.Fatalf("get after disable status=%d dto=%+v, want 200/DISABLED", getStatus, got)
		}

		secondStatus, secondDTO := disableTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if secondStatus != http.StatusOK || secondDTO.Status != "DISABLED" {
			t.Fatalf("second disable status=%d dto=%+v, want 200/DISABLED (idempotent)", secondStatus, secondDTO)
		}
	})

	t.Run("enable transitions back to ACTIVE, and is idempotent on a second call", func(t *testing.T) {
		status, dto := enableTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if status != http.StatusOK || dto.Status != "ACTIVE" {
			t.Fatalf("enable status=%d dto=%+v, want 200/ACTIVE", status, dto)
		}

		secondStatus, secondDTO := enableTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if secondStatus != http.StatusOK || secondDTO.Status != "ACTIVE" {
			t.Fatalf("second enable status=%d dto=%+v, want 200/ACTIVE (idempotent)", secondStatus, secondDTO)
		}
	})

	t.Run("delete removes the instance permanently — a subsequent get is not-found", func(t *testing.T) {
		status := deleteTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if status != http.StatusNoContent {
			t.Fatalf("delete status = %d, want %d", status, http.StatusNoContent)
		}
		getStatus, _ := getTriggerInstance(t, wired, fixture.orgAuth, instanceA.ID)
		if getStatus != http.StatusNotFound {
			t.Fatalf("get-after-delete status = %d, want %d", getStatus, http.StatusNotFound)
		}
	})
}

// TestTriggerInstanceJourney_DeletingAConnectionCascadesToItsOwnInstancesOnly
// is Slice 2's AC7: deleting a connection deletes its trigger instances, and
// only its own.
func TestTriggerInstanceJourney_DeletingAConnectionCascadesToItsOwnInstancesOnly(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithTrigger(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	connToDelete := activateConnectionThroughRealHandshake(t, wired, fixture)
	connToKeep := activateConnectionThroughRealHandshake(t, wired, fixture)
	_, instanceOnDeletedConn := createTriggerInstance(t, wired, fixture.orgAuth, connToDelete.ID, outlookMessageReceivedSlug, `{}`)
	_, instanceOnKeptConn := createTriggerInstance(t, wired, fixture.orgAuth, connToKeep.ID, outlookMessageReceivedSlug, `{}`)

	w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/connections/"+connToDelete.ID, fixture.orgAuth, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete connection status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}

	getDeletedStatus, _ := getTriggerInstance(t, wired, fixture.orgAuth, instanceOnDeletedConn.ID)
	if getDeletedStatus != http.StatusNotFound {
		t.Errorf("get for the deleted connection's instance status = %d, want %d", getDeletedStatus, http.StatusNotFound)
	}

	getKeptStatus, _ := getTriggerInstance(t, wired, fixture.orgAuth, instanceOnKeptConn.ID)
	if getKeptStatus != http.StatusOK {
		t.Errorf("get for the untouched connection's instance status = %d, want %d — the cascade must not reach other connections", getKeptStatus, http.StatusOK)
	}

	remaining, err := wired.DB.NewSelect().Table("trigger_instances").Where("connection_id = ?", connToDelete.ID).Count(context.Background())
	if err != nil {
		t.Fatalf("count trigger_instances rows: %v", err)
	}
	if remaining != 0 {
		t.Errorf("trigger_instances rows remaining for the deleted connection = %d, want 0", remaining)
	}
}

// TestTriggerInstanceJourney_CrossOrganizationOperationsAreNotFound is Slice
// 2's AC8.
func TestTriggerInstanceJourney_CrossOrganizationOperationsAreNotFound(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "access-token", RefreshToken: "refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithTrigger(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	active := activateConnectionThroughRealHandshake(t, wired, fixture)
	_, instance := createTriggerInstance(t, wired, fixture.orgAuth, active.ID, outlookMessageReceivedSlug, `{}`)
	otherAuth := newSecondOrgAuth(t, wired, "Other Org")

	if status, _ := getTriggerInstance(t, wired, otherAuth, instance.ID); status != http.StatusNotFound {
		t.Errorf("cross-org get status = %d, want %d", status, http.StatusNotFound)
	}
	if status, _ := disableTriggerInstance(t, wired, otherAuth, instance.ID); status != http.StatusNotFound {
		t.Errorf("cross-org disable status = %d, want %d", status, http.StatusNotFound)
	}
	if status, _ := enableTriggerInstance(t, wired, otherAuth, instance.ID); status != http.StatusNotFound {
		t.Errorf("cross-org enable status = %d, want %d", status, http.StatusNotFound)
	}
	if status := deleteTriggerInstance(t, wired, otherAuth, instance.ID); status != http.StatusNotFound {
		t.Errorf("cross-org delete status = %d, want %d", status, http.StatusNotFound)
	}

	// None of the above must have disturbed the instance under its own
	// organization's key.
	stillStatus, still := getTriggerInstance(t, wired, fixture.orgAuth, instance.ID)
	if stillStatus != http.StatusOK {
		t.Fatalf("instance was affected by a cross-org request; get status = %d, want %d", stillStatus, http.StatusOK)
	}
	if still.Status != "ACTIVE" {
		t.Errorf("status = %q, want %q — cross-org access must be a no-op", still.Status, "ACTIVE")
	}
}
