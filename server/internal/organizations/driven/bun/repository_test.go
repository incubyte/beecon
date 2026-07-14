// Package bun_test exercises the bun-backed organizations Repository's
// ListAll (Slice 1, PD40) directly against a real SQLite database, mirroring
// connections/driven/bun's own reasoning: ordering and cursor-boundary
// comparisons are SQL-level guarantees ("created_at DESC, id DESC", the
// cursor's "< OR (= AND <)" predicate) the driven/memory fake's plain sort
// cannot itself prove is what actually runs against the database.
package bun_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/db"
	"beecon/internal/organizations"
	organizationsbun "beecon/internal/organizations/driven/bun"
)

var testDSNCounter int64

// newTestRepository boots a fresh in-memory SQLite database, runs the real
// embedded migrations, and returns a bun-backed Repository.
func newTestRepository(t *testing.T) *organizationsbun.Repository {
	t.Helper()
	n := atomic.AddInt64(&testDSNCounter, 1)
	dsn := fmt.Sprintf("file:organizations_listall_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return organizationsbun.NewRepository(database)
}

// seedOrganization inserts one organization row with an explicit id and
// createdAt, bypassing the domain constructor so tests can place rows at
// exact, controlled points in the created_at ordering.
func seedOrganization(t *testing.T, repo *organizationsbun.Repository, id string, createdAt time.Time) organizations.Organization {
	t.Helper()
	org := organizations.Organization{
		ID:                  organizations.OrgID(id),
		Name:                "Org " + id,
		AllowedRedirectURIs: []string{},
		CreatedAt:           createdAt,
	}
	if err := repo.Save(context.Background(), org); err != nil {
		t.Fatalf("seed organization %s: %v", id, err)
	}
	return org
}

func idsOf(orgs []organizations.Organization) []string {
	ids := make([]string, len(orgs))
	for i, org := range orgs {
		ids[i] = string(org.ID)
	}
	return ids
}

// TestListAll_OrdersByCreatedAtDescendingThenIdDescending pins the exact SQL
// ordering (Slice 1, PD40): three distinct created_at values come back
// newest first, and two rows sharing the same created_at tiebreak on id
// descending.
func TestListAll_OrdersByCreatedAtDescendingThenIdDescending(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedOrganization(t, repo, "org_oldest", base)
	seedOrganization(t, repo, "org_middle", base.Add(time.Hour))
	// Two rows sharing the same (newest) created_at: the higher id must sort
	// first, exercising the tiebreak independent of the created_at compare.
	seedOrganization(t, repo, "org_newest_a", base.Add(2*time.Hour))
	seedOrganization(t, repo, "org_newest_b", base.Add(2*time.Hour))

	got, err := repo.ListAll(context.Background(), nil, 10)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"org_newest_b", "org_newest_a", "org_middle", "org_oldest"}
	if got := idsOf(got); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestListAll_LimitBoundsTheNumberOfRowsReturned confirms limit is applied
// at the SQL level (a plain LIMIT clause), independent of how many rows
// exist.
func TestListAll_LimitBoundsTheNumberOfRowsReturned(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedOrganization(t, repo, fmt.Sprintf("org_%d", i), base.Add(time.Duration(i)*time.Minute))
	}

	got, err := repo.ListAll(context.Background(), nil, 2)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (the requested limit)", len(got))
	}
}

// TestListAll_CursorExcludesRowsAtOrAfterTheCursorPosition pins the cursor
// predicate directly: given the cursor of the second-newest row, the next
// page must contain only strictly older rows (by the same created_at/id
// ordering), never the cursor row itself or anything newer.
func TestListAll_CursorExcludesRowsAtOrAfterTheCursorPosition(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedOrganization(t, repo, "org_a", base)
	seedOrganization(t, repo, "org_b", base.Add(time.Hour))
	seedOrganization(t, repo, "org_c", base.Add(2*time.Hour))

	firstPage, err := repo.ListAll(context.Background(), nil, 1)
	if err != nil {
		t.Fatalf("first page: unexpected error: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != "org_c" {
		t.Fatalf("first page = %v, want [org_c]", idsOf(firstPage))
	}
	cursor := &organizations.ListAllCursor{CreatedAt: firstPage[0].CreatedAt, ID: firstPage[0].ID}

	secondPage, err := repo.ListAll(context.Background(), cursor, 10)

	if err != nil {
		t.Fatalf("second page: unexpected error: %v", err)
	}
	want := []string{"org_b", "org_a"}
	if got := idsOf(secondPage); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("second page = %v, want %v (org_c must never reappear)", got, want)
	}
}

// TestListAll_CursorAtTheSameCreatedAtTiebreaksOnIdAtTheSQLLevel: two rows
// sharing one created_at value must still resolve unambiguously across a
// page boundary purely via the "(created_at = ? AND id < ?)" clause — the
// scenario the newest-first-ordering test's tiebreak requires the cursor
// predicate itself to also honor across two separate ListAll calls.
func TestListAll_CursorAtTheSameCreatedAtTiebreaksOnIdAtTheSQLLevel(t *testing.T) {
	repo := newTestRepository(t)
	same := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedOrganization(t, repo, "org_1", same)
	seedOrganization(t, repo, "org_2", same)
	seedOrganization(t, repo, "org_3", same)

	firstPage, err := repo.ListAll(context.Background(), nil, 1)
	if err != nil {
		t.Fatalf("first page: unexpected error: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != "org_3" {
		t.Fatalf("first page = %v, want [org_3] (highest id wins the same-created_at tiebreak)", idsOf(firstPage))
	}
	cursor := &organizations.ListAllCursor{CreatedAt: firstPage[0].CreatedAt, ID: firstPage[0].ID}

	rest, err := repo.ListAll(context.Background(), cursor, 10)

	if err != nil {
		t.Fatalf("second page: unexpected error: %v", err)
	}
	want := []string{"org_2", "org_1"}
	if got := idsOf(rest); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("remaining rows = %v, want %v", got, want)
	}
}

// --- UserRepository.ListByOrg (Slice 4, PD40) directly against real SQLite:
// the same reasoning as ListAll's own bun-level tests above — ordering and
// the cursor-boundary predicate, plus org-scoping (the "WHERE org_id = ?"
// clause), are SQL-level guarantees the driven/memory fake's plain sort and
// map-filter cannot themselves prove is what actually runs against the
// database. ---

// seedUser inserts one user row with an explicit id, org, and createdAt,
// bypassing the domain constructor so tests can place rows at exact,
// controlled points in the created_at ordering and across organizations.
func seedUser(t *testing.T, repo *organizationsbun.Repository, id, org string, createdAt time.Time) organizations.User {
	t.Helper()
	user := organizations.User{
		ID:        organizations.UserID(id),
		OrgID:     organizations.OrgID(org),
		Name:      "User " + id,
		CreatedAt: createdAt,
	}
	if err := repo.SaveUser(context.Background(), user); err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
	return user
}

func userIDsOf(users []organizations.User) []string {
	ids := make([]string, len(users))
	for i, user := range users {
		ids[i] = string(user.ID)
	}
	return ids
}

// TestListByOrg_OrdersByCreatedAtDescendingThenIdDescending mirrors
// TestListAll_OrdersByCreatedAtDescendingThenIdDescending for the org-scoped
// user listing.
func TestListByOrg_OrdersByCreatedAtDescendingThenIdDescending(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedUser(t, repo, "user_oldest", "org_1", base)
	seedUser(t, repo, "user_middle", "org_1", base.Add(time.Hour))
	seedUser(t, repo, "user_newest_a", "org_1", base.Add(2*time.Hour))
	seedUser(t, repo, "user_newest_b", "org_1", base.Add(2*time.Hour))

	got, err := repo.ListByOrg(context.Background(), "org_1", nil, 10)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"user_newest_b", "user_newest_a", "user_middle", "user_oldest"}
	if got := userIDsOf(got); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestListByOrg_OnlyReturnsRowsBelongingToTheRequestedOrg is the org-scoping
// guarantee at the SQL level: a user seeded under a different organization
// must never appear, no matter how its created_at/id would otherwise sort.
func TestListByOrg_OnlyReturnsRowsBelongingToTheRequestedOrg(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedUser(t, repo, "user_org_a_1", "org_a", base)
	seedUser(t, repo, "user_org_a_2", "org_a", base.Add(time.Hour))
	seedUser(t, repo, "user_org_b_1", "org_b", base.Add(2*time.Hour)) // newest overall, but a different org

	got, err := repo.ListByOrg(context.Background(), "org_a", nil, 10)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"user_org_a_2", "user_org_a_1"}
	if gotIDs := userIDsOf(got); fmt.Sprint(gotIDs) != fmt.Sprint(want) {
		t.Fatalf("org_a's users = %v, want %v (org_b's newer row must never appear)", gotIDs, want)
	}
}

// TestListByOrg_LimitBoundsTheNumberOfRowsReturned mirrors
// TestListAll_LimitBoundsTheNumberOfRowsReturned.
func TestListByOrg_LimitBoundsTheNumberOfRowsReturned(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedUser(t, repo, fmt.Sprintf("user_%d", i), "org_1", base.Add(time.Duration(i)*time.Minute))
	}

	got, err := repo.ListByOrg(context.Background(), "org_1", nil, 2)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (the requested limit)", len(got))
	}
}

// TestListByOrg_CursorExcludesRowsAtOrAfterTheCursorPosition mirrors
// TestListAll_CursorExcludesRowsAtOrAfterTheCursorPosition for the org-scoped
// user listing's own cursor predicate.
func TestListByOrg_CursorExcludesRowsAtOrAfterTheCursorPosition(t *testing.T) {
	repo := newTestRepository(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedUser(t, repo, "user_a", "org_1", base)
	seedUser(t, repo, "user_b", "org_1", base.Add(time.Hour))
	seedUser(t, repo, "user_c", "org_1", base.Add(2*time.Hour))

	firstPage, err := repo.ListByOrg(context.Background(), "org_1", nil, 1)
	if err != nil {
		t.Fatalf("first page: unexpected error: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].ID != "user_c" {
		t.Fatalf("first page = %v, want [user_c]", userIDsOf(firstPage))
	}
	cursor := &organizations.UserListCursor{CreatedAt: firstPage[0].CreatedAt, ID: firstPage[0].ID}

	secondPage, err := repo.ListByOrg(context.Background(), "org_1", cursor, 10)

	if err != nil {
		t.Fatalf("second page: unexpected error: %v", err)
	}
	want := []string{"user_b", "user_a"}
	if got := userIDsOf(secondPage); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("second page = %v, want %v (user_c must never reappear)", got, want)
	}
}

// --- GovernanceRepository (Slice 5, migration 0018) directly against real
// SQLite: FindByOrg/SaveGovernance's JSON-encoded text-column round trip and
// the nullable allow_list column (PD42's "inherit all" state) are SQL-level
// guarantees the driven/memory fake's plain map cannot itself prove is what
// actually runs against the database. ---

// TestFindByOrg_ReturnsNilForAnOrganizationWithNoGovernanceRow is PD42's
// continuity guarantee at the raw repository layer: a miss is (nil, nil),
// not an error — the facade's GetGovernance synthesizes the default from
// this.
func TestFindByOrg_ReturnsNilForAnOrganizationWithNoGovernanceRow(t *testing.T) {
	repo := newTestRepository(t)

	got, err := repo.FindByOrg(context.Background(), "org_never_configured")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("FindByOrg = %+v, want nil for an organization with no governance row", got)
	}
}

// TestSaveGovernance_InsertsANewRowThenFindByOrgRoundTripsEveryField pins the
// JSON-encoded array columns (allow_list/hidden/featured) and the
// featured_cap integer column round-tripping exactly through real SQLite.
func TestSaveGovernance_InsertsANewRowThenFindByOrgRoundTripsEveryField(t *testing.T) {
	repo := newTestRepository(t)
	allowList := []string{"intg_1", "intg_2"}
	governance := organizations.Governance{
		OrgID:       "org_1",
		AllowList:   &allowList,
		Hidden:      []string{"intg_3"},
		Featured:    []string{"intg_1"},
		FeaturedCap: 5,
	}

	if err := repo.SaveGovernance(context.Background(), governance); err != nil {
		t.Fatalf("SaveGovernance: %v", err)
	}

	got, err := repo.FindByOrg(context.Background(), "org_1")

	if err != nil {
		t.Fatalf("FindByOrg: %v", err)
	}
	if got == nil {
		t.Fatal("FindByOrg = nil, want the saved row")
	}
	if got.AllowList == nil || len(*got.AllowList) != 2 || (*got.AllowList)[0] != "intg_1" || (*got.AllowList)[1] != "intg_2" {
		t.Errorf("AllowList = %v, want [intg_1 intg_2]", got.AllowList)
	}
	if len(got.Hidden) != 1 || got.Hidden[0] != "intg_3" {
		t.Errorf("Hidden = %v, want [intg_3]", got.Hidden)
	}
	if len(got.Featured) != 1 || got.Featured[0] != "intg_1" {
		t.Errorf("Featured = %v, want [intg_1]", got.Featured)
	}
	if got.FeaturedCap != 5 {
		t.Errorf("FeaturedCap = %d, want 5", got.FeaturedCap)
	}
}

// TestSaveGovernance_ANilAllowListPersistsAndReadsBackAsNil is PD42's
// "inherit the full catalog" state, proven through a fresh repository handle
// against the same database: FindByOrg must read the allow_list column back
// as a true nil, not a non-nil pointer to an empty/null-decoded slice —
// governanceFromRow's `row.AllowList != nil` branch only takes that path when
// the column itself is SQL NULL, so this pins the column actually persisting
// as NULL rather than the literal JSON string "null".
func TestSaveGovernance_ANilAllowListPersistsAndReadsBackAsNil(t *testing.T) {
	repo := newTestRepository(t)
	governance := organizations.Governance{OrgID: "org_1", FeaturedCap: 8}

	if err := repo.SaveGovernance(context.Background(), governance); err != nil {
		t.Fatalf("SaveGovernance: %v", err)
	}

	got, err := repo.FindByOrg(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("FindByOrg: %v", err)
	}
	if got == nil {
		t.Fatal("FindByOrg = nil, want the saved row")
	}
	if got.AllowList != nil {
		t.Errorf("AllowList = %v, want nil", got.AllowList)
	}
}

// TestSaveGovernance_ASecondCallUpsertsRatherThanDuplicatingTheRow proves the
// find-then-insert-or-update convention (organization_id is the primary
// key): calling SaveGovernance twice for the same org must replace the row's
// values, not violate the primary key or leave two rows behind.
func TestSaveGovernance_ASecondCallUpsertsRatherThanDuplicatingTheRow(t *testing.T) {
	repo := newTestRepository(t)
	firstAllowList := []string{"intg_1"}
	if err := repo.SaveGovernance(context.Background(), organizations.Governance{
		OrgID: "org_1", AllowList: &firstAllowList, FeaturedCap: 8,
	}); err != nil {
		t.Fatalf("first SaveGovernance: %v", err)
	}

	secondAllowList := []string{"intg_2", "intg_3"}
	if err := repo.SaveGovernance(context.Background(), organizations.Governance{
		OrgID: "org_1", AllowList: &secondAllowList, Hidden: []string{"intg_4"}, FeaturedCap: 3,
	}); err != nil {
		t.Fatalf("second SaveGovernance: %v", err)
	}

	got, err := repo.FindByOrg(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("FindByOrg: %v", err)
	}
	if got.AllowList == nil || len(*got.AllowList) != 2 || (*got.AllowList)[0] != "intg_2" {
		t.Errorf("AllowList = %v, want the second call's [intg_2 intg_3], not a merge of both calls", got.AllowList)
	}
	if len(got.Hidden) != 1 || got.Hidden[0] != "intg_4" {
		t.Errorf("Hidden = %v, want [intg_4]", got.Hidden)
	}
	if got.FeaturedCap != 3 {
		t.Errorf("FeaturedCap = %d, want the second call's 3", got.FeaturedCap)
	}
	// A third SaveGovernance succeeding at all (organization_id is the
	// table's primary key) confirms the second call above updated the
	// existing row rather than a second INSERT violating that primary key.
	if err := repo.SaveGovernance(context.Background(), organizations.Governance{OrgID: "org_1", FeaturedCap: 1}); err != nil {
		t.Fatalf("third SaveGovernance (would fail on a primary-key violation from a duplicated row): %v", err)
	}
}

// TestSaveGovernance_IsOrgScopedAtTheSQLLevel proves two organizations'
// governance rows never collide or bleed into each other's FindByOrg result
// (Slice 5's isolation AC, at the raw repository layer).
func TestSaveGovernance_IsOrgScopedAtTheSQLLevel(t *testing.T) {
	repo := newTestRepository(t)
	allowListA := []string{"intg_a_only"}
	if err := repo.SaveGovernance(context.Background(), organizations.Governance{
		OrgID: "org_a", AllowList: &allowListA, FeaturedCap: 8,
	}); err != nil {
		t.Fatalf("SaveGovernance org_a: %v", err)
	}

	gotB, err := repo.FindByOrg(context.Background(), "org_b")

	if err != nil {
		t.Fatalf("FindByOrg org_b: %v", err)
	}
	if gotB != nil {
		t.Fatalf("FindByOrg(org_b) = %+v, want nil — org_a's row must never surface under a different organization_id", gotB)
	}
}
