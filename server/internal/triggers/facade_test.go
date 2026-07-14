// Package triggers_test exercises triggers.Facade against the in-memory
// Repository and hand-written fakes for the two narrow cross-module reader
// ports (DefinitionReader satisfied by catalog, ConnectionReader satisfied
// by connections) — this keeps Create/List/Get/Disable/Enable/Delete's own
// orchestration isolated from catalog/connections' own domain logic, which
// is covered by their own package tests. HTTP-level and cross-module wiring
// coverage (real catalog + real connections facades, real DB) lives in
// test/crucial_path/trigger_instances_journey_integration_test.go.
package triggers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
	memory "beecon/internal/triggers/driven/memory"
)

const (
	testOrg  = organizations.OrgID("org_1")
	otherOrg = organizations.OrgID("org_2")
	testUser = organizations.UserID("user_1")
)

// outlookMessageReceivedSlug/outlookMessageReceivedSchema mirror the real
// outlook-message-received trigger declared in
// catalog/providers/outlook.yaml (PD35): folderId is a string with a
// default, nothing required — the same config schema Create validates
// against in production.
const outlookMessageReceivedSlug = "outlook-message-received"

func outlookMessageReceivedDefinition() catalog.TriggerDefinitionSummary {
	return catalog.TriggerDefinitionSummary{
		Slug: outlookMessageReceivedSlug,
		Name: "New message received",
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
		Ingestion: "poll",
	}
}

type fakeDefinitionReader struct {
	bySlug map[string]catalog.TriggerDefinitionSummary
}

func (f fakeDefinitionReader) TriggerDefinitionDetail(_ context.Context, slug string) (catalog.TriggerDefinitionSummary, error) {
	definition, ok := f.bySlug[slug]
	if !ok {
		return catalog.TriggerDefinitionSummary{}, catalog.ErrTriggerDefinitionNotFound()
	}
	return definition, nil
}

func newFakeDefinitionReader(definitions ...catalog.TriggerDefinitionSummary) fakeDefinitionReader {
	bySlug := map[string]catalog.TriggerDefinitionSummary{}
	for _, d := range definitions {
		bySlug[d.Slug] = d
	}
	return fakeDefinitionReader{bySlug: bySlug}
}

// connectionKey is keyed by org+id (not id alone) so the fake can hold two
// different organizations' connections that happen to share the same id —
// exactly the shape TestDeleteByConnection_RemovesOnlyInstancesForThatConnectionWithinThatOrganization
// needs to prove the cascade never crosses organizations.
type connectionKey struct {
	org organizations.OrgID
	id  connections.ConnectionID
}

type fakeConnectionReader struct {
	byKey map[connectionKey]connections.Connection
}

func (f fakeConnectionReader) Get(_ context.Context, org organizations.OrgID, id connections.ConnectionID) (connections.Connection, error) {
	conn, ok := f.byKey[connectionKey{org: org, id: id}]
	if !ok {
		return connections.Connection{}, connections.ErrNotFound()
	}
	return conn, nil
}

func newFakeConnectionReader(conns ...connections.Connection) fakeConnectionReader {
	byKey := map[connectionKey]connections.Connection{}
	for _, c := range conns {
		byKey[connectionKey{org: c.OrgID, id: c.ID}] = c
	}
	return fakeConnectionReader{byKey: byKey}
}

func activeConnection(id connections.ConnectionID, org organizations.OrgID, user organizations.UserID) connections.Connection {
	return connections.Connection{ID: id, OrgID: org, UserID: user, Status: connections.StatusActive, ProviderSlug: "outlook"}
}

// mutableClock lets List's cursor-pagination tests mint instances with
// distinct, strictly increasing CreatedAt timestamps.
type mutableClock struct{ now time.Time }

func (c *mutableClock) Now() time.Time { return c.now }
func (c *mutableClock) Advance(d time.Duration) time.Time {
	c.now = c.now.Add(d)
	return c.now
}

func newTriggersFacade(definitions fakeDefinitionReader, conns fakeConnectionReader) (*triggers.Facade, *mutableClock) {
	clock := &mutableClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions: definitions,
		Connections: conns,
		Now:         clock.Now,
	})
	return facade, clock
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) *httpx.DomainError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Fatalf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Fatalf("error status = %d, want %d", de.Status, wantStatus)
	}
	return de
}

// --- Create (PD33 AC1, AC2, AC3) ---

func TestCreate_BornActiveWithAStableIDAndTheConnectionsOwnUserID(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))

	instance, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{"folderId": "Inbox"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instance.ID == "" || instance.ID[:4] != "trg_" {
		t.Errorf("ID = %q, want a trg_-prefixed id", instance.ID)
	}
	if instance.Status != triggers.StatusActive {
		t.Errorf("Status = %q, want %q", instance.Status, triggers.StatusActive)
	}
	if instance.UserID != testUser {
		t.Errorf("UserID = %q, want the connection's own user %q (no independent owner)", instance.UserID, testUser)
	}
	if instance.ConnectionID != conn.ID {
		t.Errorf("ConnectionID = %q, want %q", instance.ConnectionID, conn.ID)
	}
}

func TestCreate_PersistsTheInstanceRetrievableViaGet(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	created, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{"folderId": "Archive"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.Get(context.Background(), testOrg, created.ID)

	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TriggerSlug != outlookMessageReceivedSlug {
		t.Errorf("TriggerSlug = %q, want %q", got.TriggerSlug, outlookMessageReceivedSlug)
	}
	if got.Config["folderId"] != "Archive" {
		t.Errorf("Config[folderId] = %v, want %q", got.Config["folderId"], "Archive")
	}
}

func TestCreate_RejectsConfigThatFailsTheDefinitionsConfigSchemaAndPersistsNoInstance(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))

	_, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{"folderId": float64(123)},
	})

	de := assertDomainError(t, err, triggers.CodeValidationFailed, 422)
	if de.Details["field"] != "config" {
		t.Errorf("error details field = %v, want %q", de.Details["field"], "config")
	}
	result, listErr := f.List(context.Background(), testOrg, triggers.ListParams{})
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	if len(result.Items) != 0 {
		t.Fatalf("Items = %+v, want none — an invalid config must not persist an instance", result.Items)
	}
}

func TestCreate_RejectsAnUnknownTriggerSlug(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(), newFakeConnectionReader(conn))

	_, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: "does-not-exist", Config: map[string]any{},
	})

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}

func TestCreate_RejectsAnUnknownConnectionID(t *testing.T) {
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader())

	_, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: "conn_missing", TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{},
	})

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

func TestCreate_RejectsAConnectionBelongingToAnotherOrganization(t *testing.T) {
	conn := activeConnection("conn_1", otherOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))

	_, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{},
	})

	assertDomainError(t, err, connections.CodeNotFound, 404)
}

// TestCreate_RejectsANonActiveConnectionWithAStatusExplainingError is PD33's
// AC3, exercised across every non-ACTIVE status Create might see.
func TestCreate_RejectsANonActiveConnectionWithAStatusExplainingError(t *testing.T) {
	statuses := []connections.Status{connections.StatusInitiated, connections.StatusExpired, connections.StatusDisconnected}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			conn := activeConnection("conn_1", testOrg, testUser)
			conn.Status = status
			f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))

			_, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
				ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{},
			})

			de := assertDomainError(t, err, triggers.CodeValidationFailed, 422)
			if de.Details["field"] != "connectionId" {
				t.Errorf("error details field = %v, want %q", de.Details["field"], "connectionId")
			}
			issue, _ := de.Details["issue"].(string)
			if issue == "" || issue != "connection is "+string(status) {
				t.Errorf("error details issue = %q, want it to name the actual status %q", issue, status)
			}
		})
	}
}

// --- Get (PD33 AC4, AC8) ---

func TestGet_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _ := newTriggersFacade(newFakeDefinitionReader(), newFakeConnectionReader())

	_, err := f.Get(context.Background(), testOrg, "trg_missing")

	assertDomainError(t, err, triggers.CodeNotFound, 404)
}

func TestGet_ReturnsNotFoundForAnInstanceBelongingToAnotherOrganization(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	created, err := f.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: conn.ID, TriggerSlug: outlookMessageReceivedSlug, Config: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = f.Get(context.Background(), otherOrg, created.ID)

	assertDomainError(t, err, triggers.CodeNotFound, 404)
}

// --- List (PD33 AC4) ---

func TestList_FiltersByConnectionID(t *testing.T) {
	connA := activeConnection("conn_a", testOrg, testUser)
	connB := activeConnection("conn_b", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(connA, connB))
	mustCreate(t, f, testOrg, connA.ID, map[string]any{})
	instanceB := mustCreate(t, f, testOrg, connB.ID, map[string]any{})

	result, err := f.List(context.Background(), testOrg, triggers.ListParams{ConnectionID: string(connB.ID)})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != instanceB.ID {
		t.Fatalf("Items = %+v, want exactly the one instance on %q", result.Items, connB.ID)
	}
}

func TestList_FiltersByUserID(t *testing.T) {
	const otherUser = organizations.UserID("user_2")
	connA := activeConnection("conn_a", testOrg, testUser)
	connB := activeConnection("conn_b", testOrg, otherUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(connA, connB))
	mustCreate(t, f, testOrg, connA.ID, map[string]any{})
	instanceB := mustCreate(t, f, testOrg, connB.ID, map[string]any{})

	result, err := f.List(context.Background(), testOrg, triggers.ListParams{UserID: string(otherUser)})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != instanceB.ID {
		t.Fatalf("Items = %+v, want exactly the one instance owned by %q", result.Items, otherUser)
	}
}

func TestList_IsScopedToTheCallersOrganization(t *testing.T) {
	connA := activeConnection("conn_a", testOrg, testUser)
	connB := activeConnection("conn_b", otherOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(connA, connB))
	mustCreate(t, f, testOrg, connA.ID, map[string]any{})
	mustCreate(t, f, otherOrg, connB.ID, map[string]any{})

	result, err := f.List(context.Background(), testOrg, triggers.ListParams{})

	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Items = %+v, want exactly one — otherOrg's instance must not leak", result.Items)
	}
}

func TestList_CursorPaginatesNewestFirstWithNoDuplicatesOrGaps(t *testing.T) {
	conn := activeConnection("conn_a", testOrg, testUser)
	f, clock := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	var created []triggers.TriggerInstance
	for i := 0; i < 3; i++ {
		created = append(created, mustCreate(t, f, testOrg, conn.ID, map[string]any{}))
		clock.Advance(time.Minute)
	}

	seen := map[triggers.TriggerInstanceID]bool{}
	cursor := ""
	for page := 0; page < 5; page++ {
		result, err := f.List(context.Background(), testOrg, triggers.ListParams{Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatalf("List: %v", err)
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
	if len(seen) != len(created) {
		t.Fatalf("walked %d instances, want exactly %d (no duplicates or gaps)", len(seen), len(created))
	}
	if !seen[created[0].ID] || !seen[created[len(created)-1].ID] {
		t.Fatalf("pagination missed an instance: seen=%v", seen)
	}
}

// --- Disable / Enable (PD33 AC5) ---

func TestDisable_TransitionsAnActiveInstanceToDisabled(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})

	disabled, err := f.Disable(context.Background(), testOrg, instance.ID)

	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if disabled.Status != triggers.StatusDisabled {
		t.Errorf("Status = %q, want %q", disabled.Status, triggers.StatusDisabled)
	}
	got, err := f.Get(context.Background(), testOrg, instance.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != triggers.StatusDisabled {
		t.Errorf("persisted Status = %q, want %q", got.Status, triggers.StatusDisabled)
	}
}

func TestEnable_TransitionsADisabledInstanceBackToActive(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})
	if _, err := f.Disable(context.Background(), testOrg, instance.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	enabled, err := f.Enable(context.Background(), testOrg, instance.ID)

	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if enabled.Status != triggers.StatusActive {
		t.Errorf("Status = %q, want %q", enabled.Status, triggers.StatusActive)
	}
}

// TestDisable_IsIdempotentOnAnAlreadyDisabledInstance pins the implemented
// behavior (PD33 leaves it unspecified beyond "disable stops firing"):
// disabling twice succeeds and leaves the instance DISABLED, rather than
// erroring on the second call.
func TestDisable_IsIdempotentOnAnAlreadyDisabledInstance(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})
	if _, err := f.Disable(context.Background(), testOrg, instance.ID); err != nil {
		t.Fatalf("first Disable: %v", err)
	}

	disabledAgain, err := f.Disable(context.Background(), testOrg, instance.ID)

	if err != nil {
		t.Fatalf("second Disable: unexpected error: %v", err)
	}
	if disabledAgain.Status != triggers.StatusDisabled {
		t.Errorf("Status = %q, want %q", disabledAgain.Status, triggers.StatusDisabled)
	}
}

// TestEnable_IsIdempotentOnAnAlreadyActiveInstance mirrors Disable's
// idempotency for the already-ACTIVE case.
func TestEnable_IsIdempotentOnAnAlreadyActiveInstance(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})

	enabledAgain, err := f.Enable(context.Background(), testOrg, instance.ID)

	if err != nil {
		t.Fatalf("Enable on an already-ACTIVE instance: unexpected error: %v", err)
	}
	if enabledAgain.Status != triggers.StatusActive {
		t.Errorf("Status = %q, want %q", enabledAgain.Status, triggers.StatusActive)
	}
}

func TestDisable_ReturnsNotFoundForAnInstanceBelongingToAnotherOrganizationAndLeavesItUnchanged(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})

	_, err := f.Disable(context.Background(), otherOrg, instance.ID)

	assertDomainError(t, err, triggers.CodeNotFound, 404)
	got, getErr := f.Get(context.Background(), testOrg, instance.ID)
	if getErr != nil {
		t.Fatalf("Get: %v", getErr)
	}
	if got.Status != triggers.StatusActive {
		t.Error("instance was disabled via another organization's id — cross-org access must be a no-op")
	}
}

func TestEnable_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _ := newTriggersFacade(newFakeDefinitionReader(), newFakeConnectionReader())

	_, err := f.Enable(context.Background(), testOrg, "trg_missing")

	assertDomainError(t, err, triggers.CodeNotFound, 404)
}

// --- Delete (PD33 AC6, AC7) ---

func TestDelete_RemovesTheInstanceSoASubsequentGetIsNotFound(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})

	if err := f.Delete(context.Background(), testOrg, instance.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := f.Get(context.Background(), testOrg, instance.ID)
	assertDomainError(t, err, triggers.CodeNotFound, 404)
}

func TestDelete_ReturnsNotFoundForAnUnknownID(t *testing.T) {
	f, _ := newTriggersFacade(newFakeDefinitionReader(), newFakeConnectionReader())

	err := f.Delete(context.Background(), testOrg, "trg_missing")

	assertDomainError(t, err, triggers.CodeNotFound, 404)
}

func TestDelete_ReturnsNotFoundForAnInstanceBelongingToAnotherOrganizationAndLeavesItIntact(t *testing.T) {
	conn := activeConnection("conn_1", testOrg, testUser)
	f, _ := newTriggersFacade(newFakeDefinitionReader(outlookMessageReceivedDefinition()), newFakeConnectionReader(conn))
	instance := mustCreate(t, f, testOrg, conn.ID, map[string]any{})

	err := f.Delete(context.Background(), otherOrg, instance.ID)

	assertDomainError(t, err, triggers.CodeNotFound, 404)
	if _, getErr := f.Get(context.Background(), testOrg, instance.ID); getErr != nil {
		t.Fatalf("instance was deleted via another organization's id: %v", getErr)
	}
}

// TestDeleteByConnection_RemovesOnlyInstancesForThatConnectionWithinThatOrganization
// is PD33's connection-delete cascade (called by connections.Facade.Delete
// through the Dependents port in production): it must not touch other
// connections' instances, or another organization's instances bound to a
// connection with the same id.
func TestDeleteByConnection_RemovesOnlyInstancesForThatConnectionWithinThatOrganization(t *testing.T) {
	connA := activeConnection("conn_shared_id", testOrg, testUser)
	connB := activeConnection("conn_other", testOrg, testUser)
	connCrossOrg := activeConnection("conn_shared_id", otherOrg, testUser)
	f, _ := newTriggersFacade(
		newFakeDefinitionReader(outlookMessageReceivedDefinition()),
		newFakeConnectionReader(connA, connB, connCrossOrg),
	)
	targetInstance := mustCreate(t, f, testOrg, connA.ID, map[string]any{})
	otherConnInstance := mustCreate(t, f, testOrg, connB.ID, map[string]any{})
	crossOrgInstance := mustCreate(t, f, otherOrg, connCrossOrg.ID, map[string]any{})

	if err := f.DeleteByConnection(context.Background(), testOrg, connA.ID); err != nil {
		t.Fatalf("DeleteByConnection: %v", err)
	}

	if _, err := f.Get(context.Background(), testOrg, targetInstance.ID); err == nil {
		t.Error("expected the target connection's instance to be deleted")
	}
	if _, err := f.Get(context.Background(), testOrg, otherConnInstance.ID); err != nil {
		t.Errorf("another connection's instance in the same org was deleted: %v", err)
	}
	if _, err := f.Get(context.Background(), otherOrg, crossOrgInstance.ID); err != nil {
		t.Errorf("another organization's instance bound to a connection with the same id was deleted: %v", err)
	}
}

func TestDeleteByConnection_OnAConnectionWithNoInstancesIsANoOp(t *testing.T) {
	f, _ := newTriggersFacade(newFakeDefinitionReader(), newFakeConnectionReader())

	err := f.DeleteByConnection(context.Background(), testOrg, "conn_no_instances")

	if err != nil {
		t.Fatalf("expected no error deleting instances for a connection that has none, got: %v", err)
	}
}

// mustCreate creates a valid trigger instance bound to connID within org,
// failing the test immediately on error — the shared arrange-step every
// List/Disable/Enable/Delete test in this file needs.
func mustCreate(t *testing.T, f *triggers.Facade, org organizations.OrgID, connID connections.ConnectionID, config map[string]any) triggers.TriggerInstance {
	t.Helper()
	instance, err := f.Create(context.Background(), org, triggers.CreateParams{
		ConnectionID: connID, TriggerSlug: outlookMessageReceivedSlug, Config: config,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return instance
}
