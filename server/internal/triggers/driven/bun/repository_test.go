// Package bun_test exercises the bun-backed Repository directly against a
// real SQLite database: Save's "ON CONFLICT (id) DO UPDATE" is a SQL-level
// guarantee the in-memory fake's simple map-overwrite cannot prove wrong
// (mirrors connections/driven/bun/repository_test.go's own reasoning for
// MarkStateConsumed) — a mistyped conflict target or column list would
// insert a second row instead of updating the first, silently, only
// against a real dialect. DeleteByConnection's scoping is exercised here
// too, so the real WHERE clause (not just the in-memory map filter) is
// proven to scope by both connection id and organization id.
package bun_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/connections"
	"beecon/internal/db"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
	triggersbun "beecon/internal/triggers/driven/bun"
)

var testDSNCounter int64

func newTestRepository(t *testing.T) *triggersbun.Repository {
	t.Helper()
	repo, _ := newTestRepositoryWithDB(t)
	return repo
}

// newTestRepositoryWithDB is newTestRepository plus the underlying *bun.DB —
// the ClaimDuePolls lease tests below need to read the real
// poll_lease_until column directly, which the Repository's own exported
// surface deliberately never exposes (PollLeaseUntil is not a
// triggers.TriggerInstance field at all).
func newTestRepositoryWithDB(t *testing.T) (*triggersbun.Repository, *upstreambun.DB) {
	t.Helper()
	n := atomic.AddInt64(&testDSNCounter, 1)
	dsn := fmt.Sprintf("file:trigger_instances_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return triggersbun.NewRepository(database), database
}

func testInstance(id triggers.TriggerInstanceID, org organizations.OrgID, connID string) triggers.TriggerInstance {
	return triggers.TriggerInstance{
		ID:           id,
		OrgID:        org,
		UserID:       "user_1",
		ConnectionID: connections.ConnectionID("conn_" + connID),
		TriggerSlug:  "outlook-message-received",
		Config:       map[string]any{"folderId": "Inbox"},
		Status:       triggers.StatusActive,
		CreatedAt:    time.Now().UTC(),
	}
}

func TestSave_InsertsANewInstanceRetrievableByFindByID(t *testing.T) {
	repo := newTestRepository(t)
	instance := testInstance("trg_1", "org_1", "a")

	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "org_1", "trg_1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected the instance to be found")
	}
	if got.Status != triggers.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, triggers.StatusActive)
	}
}

// TestSave_OnAConflictingIDUpdatesTheStatusInPlaceRatherThanInsertingASecondRow
// pins the ON CONFLICT (id) DO UPDATE upsert path (Disable/Enable both call
// Save again against an existing id — triggers.Repository declares no
// separate Update method).
func TestSave_OnAConflictingIDUpdatesTheStatusInPlaceRatherThanInsertingASecondRow(t *testing.T) {
	repo := newTestRepository(t)
	instance := testInstance("trg_1", "org_1", "a")
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	disabled := instance.Disable()
	if err := repo.Save(context.Background(), disabled); err != nil {
		t.Fatalf("second Save (upsert): %v", err)
	}

	got, err := repo.FindByID(context.Background(), "org_1", "trg_1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != triggers.StatusDisabled {
		t.Errorf("Status = %q, want %q (the second Save must update, not insert a second row)", got.Status, triggers.StatusDisabled)
	}

	page, err := repo.ListPage(context.Background(), "org_1", triggers.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("ListPage returned %d rows, want exactly 1 — the conflicting Save must not have inserted a duplicate", len(page))
	}
}

// TestDeleteByConnection_RemovesOnlyRowsForThatConnectionWithinThatOrganization
// proves the real SQL WHERE clause scopes by both connection_id and
// organization_id — a row bound to the same connection id but a different
// organization, and a different connection's row in the same organization,
// must both survive.
func TestDeleteByConnection_RemovesOnlyRowsForThatConnectionWithinThatOrganization(t *testing.T) {
	repo := newTestRepository(t)
	target := testInstance("trg_target", "org_1", "shared")
	sameOrgOtherConn := testInstance("trg_other_conn", "org_1", "different")
	otherOrgSameConnID := testInstance("trg_cross_org", "org_2", "shared")
	for _, instance := range []triggers.TriggerInstance{target, sameOrgOtherConn, otherOrgSameConnID} {
		if err := repo.Save(context.Background(), instance); err != nil {
			t.Fatalf("seed Save(%s): %v", instance.ID, err)
		}
	}

	if err := repo.DeleteByConnection(context.Background(), "org_1", "conn_shared"); err != nil {
		t.Fatalf("DeleteByConnection: %v", err)
	}

	if got, err := repo.FindByID(context.Background(), "org_1", "trg_target"); err != nil || got != nil {
		t.Errorf("expected the target connection's instance to be gone, got %+v (err=%v)", got, err)
	}
	if got, err := repo.FindByID(context.Background(), "org_1", "trg_other_conn"); err != nil || got == nil {
		t.Errorf("a different connection's instance in the same org was deleted (err=%v)", err)
	}
	if got, err := repo.FindByID(context.Background(), "org_2", "trg_cross_org"); err != nil || got == nil {
		t.Errorf("another organization's instance bound to a connection with the same id was deleted (err=%v)", err)
	}
}

func TestDeleteByConnection_OnAConnectionWithNoRowsIsANoOp(t *testing.T) {
	repo := newTestRepository(t)

	if err := repo.DeleteByConnection(context.Background(), "org_1", "conn_does_not_exist"); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// dueInstance builds a TriggerInstance whose NextPollAt is already due as
// of now — the shape every ClaimDuePolls test below claims against.
func dueInstance(id triggers.TriggerInstanceID, org organizations.OrgID, now time.Time) triggers.TriggerInstance {
	instance := testInstance(id, org, string(id))
	instance.NextPollAt = &now
	return instance
}

// TestClaimDuePolls_LeasesADueInstanceAndSetsItsPollLeaseUntilColumn proves
// the real dual-dialect UPDATE...RETURNING claim query (section 3 of the
// architecture doc) actually writes poll_lease_until — the in-memory fake's
// own leaseUntil map cannot prove the real SQL does this.
func TestClaimDuePolls_LeasesADueInstanceAndSetsItsPollLeaseUntilColumn(t *testing.T) {
	repo, database := newTestRepositoryWithDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance := dueInstance("trg_1", "org_1", now)
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}

	claimed, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("ClaimDuePolls: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "trg_1" {
		t.Fatalf("claimed = %+v, want exactly [trg_1]", claimed)
	}

	var row triggersbun.TriggerInstanceRow
	if err := database.NewSelect().Model(&row).Where("id = ?", "trg_1").Scan(context.Background()); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	if row.PollLeaseUntil == nil || !row.PollLeaseUntil.Equal(now.Add(60*time.Second)) {
		t.Errorf("poll_lease_until = %v, want %v", row.PollLeaseUntil, now.Add(60*time.Second))
	}
}

// TestClaimDuePolls_ALeasedInstanceIsNotReclaimedUntilItsLeaseExpires is the
// real-SQLite half of the in-memory fake's own lease test
// (triggers/poll_test.go): a second claim call before the lease expires (and
// before any Save releases it) must return nothing, and reclaiming works
// again once the lease has expired.
func TestClaimDuePolls_ALeasedInstanceIsNotReclaimedUntilItsLeaseExpires(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance := dueInstance("trg_1", "org_1", now)
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}

	first, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("first ClaimDuePolls: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly 1", first)
	}

	second, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("second ClaimDuePolls: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim (still leased, no Save yet) = %+v, want none", second)
	}

	afterLease := now.Add(61 * time.Second)
	third, err := repo.ClaimDuePolls(context.Background(), afterLease, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("third ClaimDuePolls: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("third claim (after the lease expired) = %+v, want the instance reclaimable again", third)
	}
}

// TestSave_AfterAClaimClearsThePollLeaseUntilColumn proves a terminal Save
// (the end of a poll tick, success or failure) releases whatever lease
// ClaimDuePolls last set — the real SQL half of TriggerInstanceRow's own doc
// comment ("PollLeaseUntil is never set by Save... a facade-level save
// always means this tick is over").
func TestSave_AfterAClaimClearsThePollLeaseUntilColumn(t *testing.T) {
	repo, database := newTestRepositoryWithDB(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance := dueInstance("trg_1", "org_1", now)
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10); err != nil {
		t.Fatalf("ClaimDuePolls: %v", err)
	}

	rescheduled := instance
	next := now.Add(60 * time.Second)
	rescheduled.NextPollAt = &next
	if err := repo.Save(context.Background(), rescheduled); err != nil {
		t.Fatalf("terminal Save: %v", err)
	}

	var row triggersbun.TriggerInstanceRow
	if err := database.NewSelect().Model(&row).Where("id = ?", "trg_1").Scan(context.Background()); err != nil {
		t.Fatalf("scan row: %v", err)
	}
	if row.PollLeaseUntil != nil {
		t.Errorf("poll_lease_until = %v, want nil after a terminal Save", row.PollLeaseUntil)
	}

	// And the released instance is now claimable again, once due.
	claimed, err := repo.ClaimDuePolls(context.Background(), next, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("ClaimDuePolls after release: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed after release = %+v, want exactly 1", claimed)
	}
}
