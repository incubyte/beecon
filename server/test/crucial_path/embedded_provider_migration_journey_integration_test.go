//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope,
// doJSONRequest, oauthJourneyFixture/newOAuthJourneyFixture,
// outlookDefinitionAgainst, openConnectPageAndGetState,
// activateConnectionThroughRealHandshake, executionResultDTO, executeTool
// (oauth_handshake_journey_integration_test.go/
// tool_execution_journey_integration_test.go), and
// outlookMessageReceivedSlug/outlookDefinitionWithTrigger/
// createTriggerInstance/getTriggerInstance/triggerInstanceDTO
// (trigger_instances_journey_integration_test.go) — same package). This file
// tells the Phase 5 registry sub-phase's Slice 6 migration story end to end
// against the real composition root and a real SQLite database: the boot
// backfill (catalog.Facade.BackfillEmbeddedSeed, wired unconditionally at
// every boot, app/wiring.go) mints a tool_ id for an already-embedded tool
// and records the provider as activated (AC2); an already-ACTIVE connection
// keeps executing that provider's tool by slug across a reboot with no
// re-auth (AC4); an existing trigger-instance keeps resolving/staying live
// across the same reboot (AC5); the tool's tool_ id is stable across the
// reboot and resolves to the identical tool by both slug and id (AC3
// idempotency + AC6).
package crucial_path

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

// toolDetailDTO mirrors catalog/driving/httpapi/dto.go's toolSummaryDTO
// closely enough for this file's own assertions (id alongside slug).
type toolDetailDTO struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

func getToolDetail(t *testing.T, wired *app.Wired, orgAuth, idOrSlug string) (int, toolDetailDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools/"+idOrSlug, orgAuth, "")
	var dto toolDetailDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode tool detail: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

// outlookDefinitionWithToolAndTriggerForMigrationJourney is
// outlookDefinitionWithTrigger (trigger_instances_journey_integration_test.go)
// plus the same outlook-list-messages tool declaration
// outlookDefinitionWithFakeGraphTool (tool_execution_journey_integration_test.go)
// uses, pointed at fakeGraph — this file needs both a tool and a trigger on
// the one embedded provider so the boot backfill's continuity claims can be
// checked against a live connection and a live trigger-instance in the same
// journey.
func outlookDefinitionWithToolAndTriggerForMigrationJourney(fakeMS *support.FakeMicrosoft, fakeGraph *support.FakeGraph) []catalog.ProviderDefinition {
	definitions := outlookDefinitionWithTrigger(fakeMS)
	definitions[0].Tools = []catalog.ProviderTool{
		{
			Slug: "outlook-list-messages", Name: "List messages", Method: "GET", Path: fakeGraph.MessagesURL,
			InputSchema:  map[string]any{"type": "object"},
			OutputSchema: map[string]any{"type": "object"},
		},
	}
	return definitions
}

func TestEmbeddedProviderMigrationJourney_BootBackfillNeverInterruptsLiveConnectionsOrTriggerInstancesAcrossARestart(t *testing.T) {
	dsn := support.NewTestDSN(t)
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "raw-migration-journey-access-token", RefreshToken: "raw-migration-journey-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	definitions := outlookDefinitionWithToolAndTriggerForMigrationJourney(fakeMS, fakeGraph)

	// First boot: the embedded seed's own boot backfill runs automatically
	// (app/wiring.go, unconditionally, regardless of whether a registry is
	// configured — PD59/PD68) before this journey ever touches the API.
	firstBoot := support.BootAppWithProviderDefinitionsAt(t, dsn, definitions)
	fixture := newOAuthJourneyFixture(t, firstBoot)
	connection := activateConnectionThroughRealHandshake(t, firstBoot, fixture)

	var firstExecution executionResultDTO
	t.Run("before any restart, the newly-backfilled tool executes by slug for a live connection", func(t *testing.T) {
		status, dto := executeTool(t, firstBoot, fixture.orgAuth, "outlook-list-messages", fixture.userID, connection.ID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; dto=%+v", status, http.StatusOK, dto)
		}
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error=%+v", dto.Error)
		}
		firstExecution = dto
	})

	var mintedToolID string
	t.Run("the boot backfill minted a tool_ id for the embedded tool, addressable alongside its slug", func(t *testing.T) {
		status, detail := getToolDetail(t, firstBoot, fixture.orgAuth, "outlook-list-messages")
		if status != http.StatusOK {
			t.Fatalf("get tool by slug status = %d, want %d", status, http.StatusOK)
		}
		if !strings.HasPrefix(detail.ID, "tool_") {
			t.Fatalf("tool id = %q, want a tool_-prefixed id minted by the boot backfill", detail.ID)
		}
		mintedToolID = detail.ID

		byIDStatus, byID := getToolDetail(t, firstBoot, fixture.orgAuth, mintedToolID)
		if byIDStatus != http.StatusOK {
			t.Fatalf("get tool by tool_ id status = %d, want %d", byIDStatus, http.StatusOK)
		}
		if byID.Slug != "outlook-list-messages" {
			t.Errorf("looking up by tool_ id resolved to slug %q, want %q", byID.Slug, "outlook-list-messages")
		}
	})

	var triggerInstanceID string
	t.Run("a trigger-instance created against the still-live trigger definition is born ACTIVE", func(t *testing.T) {
		status, created := createTriggerInstance(t, firstBoot, fixture.orgAuth, connection.ID, outlookMessageReceivedSlug, `{"folderId":"Inbox"}`)
		if status != http.StatusCreated {
			t.Fatalf("create trigger instance status = %d, want %d", status, http.StatusCreated)
		}
		if created.Status != "ACTIVE" {
			t.Fatalf("status = %q, want %q", created.Status, "ACTIVE")
		}
		triggerInstanceID = created.ID
	})

	// Restart against the very same database, with the very same
	// (fake-pointed) provider definitions — mirroring the existing
	// EncryptPlaintextClientSecrets boot-backfill restart convention
	// elsewhere in this package (organizations_journey_integration_test.go,
	// webhook_channel_journey_integration_test.go).
	secondBoot := support.BootAppWithProviderDefinitionsAt(t, dsn, definitions)

	t.Run("AC4: the same connection, never re-authenticated, still executes the tool by slug after the reboot", func(t *testing.T) {
		status, dto := executeTool(t, secondBoot, fixture.orgAuth, "outlook-list-messages", fixture.userID, connection.ID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d; dto=%+v", status, http.StatusOK, dto)
		}
		if !dto.Successful {
			t.Fatalf("successful = false after reboot, want true (no re-auth should have been required); error=%+v", dto.Error)
		}
		if dto.Successful != firstExecution.Successful {
			t.Errorf("post-reboot execution result shape changed: before=%+v after=%+v", firstExecution, dto)
		}
	})

	t.Run("AC3: the tool_ id minted by the first boot's backfill is unchanged after a second boot's idempotent re-run", func(t *testing.T) {
		status, detail := getToolDetail(t, secondBoot, fixture.orgAuth, "outlook-list-messages")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if detail.ID != mintedToolID {
			t.Errorf("tool_ id changed across a reboot: before=%q after=%q, want stable", mintedToolID, detail.ID)
		}
	})

	t.Run("AC6: after the reboot, the tool is still addressable by both slug and tool_ id, resolving to the same tool", func(t *testing.T) {
		bySlugStatus, bySlug := getToolDetail(t, secondBoot, fixture.orgAuth, "outlook-list-messages")
		byIDStatus, byID := getToolDetail(t, secondBoot, fixture.orgAuth, mintedToolID)
		if bySlugStatus != http.StatusOK || byIDStatus != http.StatusOK {
			t.Fatalf("bySlugStatus=%d byIDStatus=%d, want both %d", bySlugStatus, byIDStatus, http.StatusOK)
		}
		if bySlug.ID != byID.ID || bySlug.Slug != byID.Slug {
			t.Errorf("lookup by slug (%+v) and by tool_ id (%+v) diverged after the reboot", bySlug, byID)
		}

		status, dto := executeTool(t, secondBoot, fixture.orgAuth, mintedToolID, fixture.userID, connection.ID, `{}`)
		if status != http.StatusOK {
			t.Fatalf("execute-by-tool_-id status = %d, want %d; dto=%+v", status, http.StatusOK, dto)
		}
		if !dto.Successful {
			t.Fatalf("execute-by-tool_-id successful = false, want true; error=%+v", dto.Error)
		}
	})

	t.Run("AC5: the trigger-instance created before the reboot still resolves and remains ACTIVE", func(t *testing.T) {
		status, got := getTriggerInstance(t, secondBoot, fixture.orgAuth, triggerInstanceID)
		if status != http.StatusOK {
			t.Fatalf("get trigger instance status = %d, want %d", status, http.StatusOK)
		}
		if got.Status != "ACTIVE" {
			t.Errorf("status = %q after reboot, want it to remain %q", got.Status, "ACTIVE")
		}
		if got.TriggerSlug != outlookMessageReceivedSlug {
			t.Errorf("triggerSlug = %q, want %q — the instance must still resolve against its own trigger definition", got.TriggerSlug, outlookMessageReceivedSlug)
		}

		disableStatus, disabled := disableTriggerInstance(t, secondBoot, fixture.orgAuth, triggerInstanceID)
		if disableStatus != http.StatusOK || disabled.Status != "DISABLED" {
			t.Fatalf("disable after reboot status=%d dto=%+v, want 200/DISABLED — the instance must still be a fully functioning lifecycle object", disableStatus, disabled)
		}
	})
}
