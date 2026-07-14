// Package organizations_test (see facade_test.go's own doc comment for why
// this must be an external test package). This file covers Facade.ListAll
// (Slice 1, PD40): the new installation-wide, cursor-paginated read over
// every organization.
package organizations_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

// tickingClock returns a func() time.Time that advances by one second on
// every call, starting at start — used instead of the package's default
// fixed clock so organizations created in sequence get distinct CreatedAt
// values, letting ordering tests actually exercise the created_at
// comparison rather than always falling through to the id tiebreak.
func tickingClock(start time.Time) func() time.Time {
	var n int64
	return func() time.Time {
		return start.Add(time.Duration(atomic.AddInt64(&n, 1)-1) * time.Second)
	}
}

func newFacadeWithTickingClock() *organizations.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{
		Now: tickingClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
	})
}

func createN(t *testing.T, f *organizations.Facade, n int) []organizations.Organization {
	t.Helper()
	created := make([]organizations.Organization, 0, n)
	for i := 0; i < n; i++ {
		org, err := f.Create(context.Background(), fmt.Sprintf("Org %d", i))
		if err != nil {
			t.Fatalf("Create(%d): unexpected error: %v", i, err)
		}
		created = append(created, org)
	}
	return created
}

func TestListAll_ReturnsAnEmptyPageWhenNoOrganizationsExist(t *testing.T) {
	f := newFacadeWithTickingClock()

	result, err := f.ListAll(context.Background(), organizations.ListAllParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Organizations) != 0 {
		t.Fatalf("Organizations = %v, want empty", result.Organizations)
	}
	if result.NextCursor != "" {
		t.Errorf("NextCursor = %q, want empty (no next page)", result.NextCursor)
	}
}

// TestListAll_OrdersNewestFirstByCreatedAt pins the newest-first ordering
// (Slice 1, PD40): later-created organizations (distinct CreatedAt via the
// ticking clock) come back before earlier ones.
func TestListAll_OrdersNewestFirstByCreatedAt(t *testing.T) {
	f := newFacadeWithTickingClock()
	created := createN(t, f, 3)

	result, err := f.ListAll(context.Background(), organizations.ListAllParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Organizations) != 3 {
		t.Fatalf("got %d organizations, want 3", len(result.Organizations))
	}
	wantOrder := []organizations.OrgID{created[2].ID, created[1].ID, created[0].ID}
	for i, want := range wantOrder {
		if result.Organizations[i].ID != want {
			t.Errorf("Organizations[%d].ID = %q, want %q (newest-first order)", i, result.Organizations[i].ID, want)
		}
	}
}

// TestListAll_DefaultsToALimitOf50WhenNoneIsRequested seeds 51 organizations
// (all sharing no cursor) and confirms exactly 50 come back with a
// non-empty NextCursor, matching every other list endpoint's PD10-style
// default page size.
func TestListAll_DefaultsToALimitOf50WhenNoneIsRequested(t *testing.T) {
	f := newFacadeWithTickingClock()
	createN(t, f, 51)

	result, err := f.ListAll(context.Background(), organizations.ListAllParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Organizations) != 50 {
		t.Fatalf("got %d organizations, want the default limit of 50", len(result.Organizations))
	}
	if result.NextCursor == "" {
		t.Error("NextCursor is empty, want a cursor for the 51st organization's page")
	}
}

// TestListAll_CapsAnOversizedRequestedLimitAt200 seeds 201 organizations and
// requests an oversized limit, confirming the result is capped at the
// platform-wide max of 200 rather than honoring the caller's larger value.
func TestListAll_CapsAnOversizedRequestedLimitAt200(t *testing.T) {
	f := newFacadeWithTickingClock()
	createN(t, f, 201)

	result, err := f.ListAll(context.Background(), organizations.ListAllParams{Limit: 500})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Organizations) != 200 {
		t.Fatalf("got %d organizations, want the max-200 cap", len(result.Organizations))
	}
	if result.NextCursor == "" {
		t.Error("NextCursor is empty, want a cursor for the remaining organization")
	}
}

// TestListAll_CursorRoundTripsAcrossPagesWithNoOverlapOrGap fetches every
// organization two at a time via NextCursor and confirms the concatenated
// pages equal exactly one full ListAll page's worth of ids, in the same
// newest-first order, with no id repeated or skipped.
func TestListAll_CursorRoundTripsAcrossPagesWithNoOverlapOrGap(t *testing.T) {
	f := newFacadeWithTickingClock()
	createN(t, f, 5)

	full, err := f.ListAll(context.Background(), organizations.ListAllParams{})
	if err != nil {
		t.Fatalf("baseline ListAll: unexpected error: %v", err)
	}

	var paged []organizations.Organization
	cursor := ""
	for {
		page, err := f.ListAll(context.Background(), organizations.ListAllParams{Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("paged ListAll (cursor=%q): unexpected error: %v", cursor, err)
		}
		paged = append(paged, page.Organizations...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(paged) != len(full.Organizations) {
		t.Fatalf("paged through %d organizations, want %d (the unpaged baseline)", len(paged), len(full.Organizations))
	}
	for i := range full.Organizations {
		if paged[i].ID != full.Organizations[i].ID {
			t.Errorf("paged[%d].ID = %q, want %q", i, paged[i].ID, full.Organizations[i].ID)
		}
	}
}

func TestListAll_RejectsAMalformedCursorAsAValidationError(t *testing.T) {
	f := newFacadeWithTickingClock()

	_, err := f.ListAll(context.Background(), organizations.ListAllParams{Cursor: "not-a-valid-cursor!!"})

	de := assertDomainError(t, err, organizations.CodeValidationFailed, 422)
	if de.Details["field"] != "cursor" {
		t.Errorf("error details field = %v, want %q", de.Details["field"], "cursor")
	}
}
