// Package organizations_test (see facade_test.go's own doc comment for why
// this must be an external test package). This file covers Facade.ListUsers
// (Slice 4, PD40): the new org-scoped, cursor-paginated end-users read —
// mirrors facade_listall_test.go's own ListAll coverage, scoped to one
// organization instead of the whole installation.
package organizations_test

import (
	"context"
	"fmt"
	"testing"

	"beecon/internal/organizations"
)

func createUsersN(t *testing.T, f *organizations.Facade, org organizations.OrgID, n int) []organizations.User {
	t.Helper()
	created := make([]organizations.User, 0, n)
	for i := 0; i < n; i++ {
		user, err := f.CreateUser(context.Background(), org, fmt.Sprintf("User %d", i), "")
		if err != nil {
			t.Fatalf("CreateUser(%d): unexpected error: %v", i, err)
		}
		created = append(created, user)
	}
	return created
}

func TestListUsers_ReturnsAnEmptyPageWhenTheOrgHasNoUsers(t *testing.T) {
	f := newFacadeWithTickingClock()

	result, err := f.ListUsers(context.Background(), organizations.OrgID("org_1"), organizations.ListUsersParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Users) != 0 {
		t.Fatalf("Users = %v, want empty", result.Users)
	}
	if result.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty (no next page)", result.NextCursor)
	}
}

func TestListUsers_OrdersNewestFirstByCreatedAt(t *testing.T) {
	f := newFacadeWithTickingClock()
	org := organizations.OrgID("org_1")
	created := createUsersN(t, f, org, 3)

	result, err := f.ListUsers(context.Background(), org, organizations.ListUsersParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Users) != 3 {
		t.Fatalf("got %d users, want 3", len(result.Users))
	}
	wantOrder := []organizations.UserID{created[2].ID, created[1].ID, created[0].ID}
	for i, want := range wantOrder {
		if result.Users[i].ID != want {
			t.Errorf("Users[%d].ID = %q, want %q (newest-first order)", i, result.Users[i].ID, want)
		}
	}
}

func TestListUsers_OnlyReturnsUsersBelongingToTheGivenOrg(t *testing.T) {
	f := newFacadeWithTickingClock()
	orgA := organizations.OrgID("org_a")
	orgB := organizations.OrgID("org_b")
	createUsersN(t, f, orgA, 2)
	createUsersN(t, f, orgB, 3)

	result, err := f.ListUsers(context.Background(), orgA, organizations.ListUsersParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Users) != 2 {
		t.Fatalf("got %d users for org A, want 2 (org B's users must not leak in)", len(result.Users))
	}
	for _, user := range result.Users {
		if user.OrgID != orgA {
			t.Errorf("user %q belongs to org %q, want only %q", user.ID, user.OrgID, orgA)
		}
	}
}

func TestListUsers_DefaultsToALimitOf50WhenNoneIsRequested(t *testing.T) {
	f := newFacadeWithTickingClock()
	org := organizations.OrgID("org_1")
	createUsersN(t, f, org, 51)

	result, err := f.ListUsers(context.Background(), org, organizations.ListUsersParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Users) != 50 {
		t.Fatalf("got %d users, want the default limit of 50", len(result.Users))
	}
	if result.NextCursor == "" {
		t.Error("NextCursor is empty, want a cursor for the 51st user's page")
	}
}

func TestListUsers_CapsAnOversizedRequestedLimitAt200(t *testing.T) {
	f := newFacadeWithTickingClock()
	org := organizations.OrgID("org_1")
	createUsersN(t, f, org, 201)

	result, err := f.ListUsers(context.Background(), org, organizations.ListUsersParams{Limit: 500})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Users) != 200 {
		t.Fatalf("got %d users, want the max-200 cap", len(result.Users))
	}
	if result.NextCursor == "" {
		t.Error("NextCursor is empty, want a cursor for the remaining user")
	}
}

func TestListUsers_CursorRoundTripsAcrossPagesWithNoOverlapOrGap(t *testing.T) {
	f := newFacadeWithTickingClock()
	org := organizations.OrgID("org_1")
	createUsersN(t, f, org, 5)

	full, err := f.ListUsers(context.Background(), org, organizations.ListUsersParams{})
	if err != nil {
		t.Fatalf("baseline ListUsers: unexpected error: %v", err)
	}

	var paged []organizations.User
	cursor := ""
	for {
		page, err := f.ListUsers(context.Background(), org, organizations.ListUsersParams{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("paged ListUsers (cursor=%q): unexpected error: %v", cursor, err)
		}
		paged = append(paged, page.Users...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(paged) != len(full.Users) {
		t.Fatalf("paged through %d users, want %d (the unpaged baseline)", len(paged), len(full.Users))
	}
	for i := range full.Users {
		if paged[i].ID != full.Users[i].ID {
			t.Errorf("paged[%d].ID = %q, want %q", i, paged[i].ID, full.Users[i].ID)
		}
	}
}

func TestListUsers_RejectsAMalformedCursorAsAValidationError(t *testing.T) {
	f := newFacadeWithTickingClock()

	_, err := f.ListUsers(context.Background(), organizations.OrgID("org_1"), organizations.ListUsersParams{Cursor: "not-a-valid-cursor!!"})

	de := assertDomainError(t, err, organizations.CodeValidationFailed, 422)
	if de.Details["field"] != "cursor" {
		t.Errorf("error details field = %v, want %q", de.Details["field"], "cursor")
	}
}

// TestListUsers_ACursorMintedForOneOrgAppliedToAnotherOrgNeverLeaksItsUsers
// guards against a cursor accidentally bypassing org-scoping: even if an
// org A cursor is (mis)applied to org B's own ListUsers call, every
// returned row must still belong to org B — the query is scoped by org on
// every call, independent of what the cursor's boundary values happen to be.
func TestListUsers_ACursorMintedForOneOrgAppliedToAnotherOrgNeverLeaksItsUsers(t *testing.T) {
	f := newFacadeWithTickingClock()
	orgA := organizations.OrgID("org_a")
	orgB := organizations.OrgID("org_b")
	createUsersN(t, f, orgA, 3)
	createUsersN(t, f, orgB, 3)

	firstPageA, err := f.ListUsers(context.Background(), orgA, organizations.ListUsersParams{Limit: 1})
	if err != nil {
		t.Fatalf("org A first page: unexpected error: %v", err)
	}
	if firstPageA.NextCursor == "" {
		t.Fatal("org A's first page carried no cursor to reuse against org B")
	}

	resultB, err := f.ListUsers(context.Background(), orgB, organizations.ListUsersParams{Cursor: firstPageA.NextCursor})

	if err != nil {
		t.Fatalf("unexpected error scoping org A's cursor against org B: %v", err)
	}
	for _, user := range resultB.Users {
		if user.OrgID != orgB {
			t.Fatalf("user %q belongs to org %q, want only %q — org A's cursor leaked into org B's result", user.ID, user.OrgID, orgB)
		}
	}
}
