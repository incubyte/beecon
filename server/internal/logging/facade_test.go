// Package logging_test exercises logging.Facade (Record, Query) against the
// in-memory Repository: AC8's log-entry shape, AC9's redaction happening
// before persistence, and AC10's cursor pagination and org isolation.
package logging_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"beecon/internal/logging"
	memory "beecon/internal/logging/driven/memory"
	"beecon/internal/organizations"
)

const (
	testOrg  = organizations.OrgID("org_1")
	otherOrg = organizations.OrgID("org_2")
)

func newFacade() *logging.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{})
}

func recordInput(org organizations.OrgID, overrides func(*logging.RecordInput)) logging.RecordInput {
	in := logging.RecordInput{
		OrgID:        org,
		UserID:       "user_1",
		ConnectionID: "conn_1",
		ToolSlug:     "outlook-list-messages",
		Kind:         logging.KindToolExecution,
		Status:       200,
		DurationMs:   42,
		RequestBody:  `{"method":"GET"}`,
		ResponseBody: `{"value":[]}`,
	}
	if overrides != nil {
		overrides(&in)
	}
	return in
}

// --- AC8: recording ---

func TestRecord_PersistsAToolExecutionEntryQueryableAfterward(t *testing.T) {
	f := newFacade()

	err := f.Record(context.Background(), recordInput(testOrg, nil))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.OrgID != testOrg {
		t.Errorf("OrgID = %q, want %q", entry.OrgID, testOrg)
	}
	if entry.UserID != "user_1" {
		t.Errorf("UserID = %q, want %q", entry.UserID, "user_1")
	}
	if entry.ConnectionID != "conn_1" {
		t.Errorf("ConnectionID = %q, want %q", entry.ConnectionID, "conn_1")
	}
	if entry.ToolSlug != "outlook-list-messages" {
		t.Errorf("ToolSlug = %q, want %q", entry.ToolSlug, "outlook-list-messages")
	}
	if entry.Status != 200 {
		t.Errorf("Status = %d, want %d", entry.Status, 200)
	}
	if entry.DurationMs != 42 {
		t.Errorf("DurationMs = %d, want %d", entry.DurationMs, 42)
	}
	if entry.ID == "" {
		t.Error("ID must not be empty")
	}
	if entry.CreatedAt.IsZero() {
		t.Error("CreatedAt must not be zero")
	}
}

func TestRecord_PersistsAnOAuthTokenExchangeEntryWithNoToolSlug(t *testing.T) {
	f := newFacade()

	err := f.Record(context.Background(), recordInput(testOrg, func(in *logging.RecordInput) {
		in.Kind = logging.KindOAuthTokenExchange
		in.ToolSlug = ""
	}))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	if result.Entries[0].Kind != logging.KindOAuthTokenExchange {
		t.Errorf("Kind = %q, want %q", result.Entries[0].Kind, logging.KindOAuthTokenExchange)
	}
	if result.Entries[0].ToolSlug != "" {
		t.Errorf("ToolSlug = %q, want empty for an OAuth token-exchange entry", result.Entries[0].ToolSlug)
	}
}

// TestRecord_PersistsTheRateLimitedFlagOntoTheStoredEntry is Slice 6's (PD21)
// half of AC8/AC5: RecordInput.RateLimited must actually reach the stored
// EventLog, not just exist as an unused field — proven from both sides (a
// rate-limited attempt persists true, a normal one persists false).
func TestRecord_PersistsTheRateLimitedFlagOntoTheStoredEntry(t *testing.T) {
	f := newFacade()
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) {
		in.Status = 429
		in.RateLimited = true
	}))
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) {
		in.Status = 200
		in.RateLimited = false
	}))

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(result.Entries))
	}
	for _, entry := range result.Entries {
		switch entry.Status {
		case 429:
			if !entry.RateLimited {
				t.Errorf("entry %+v has RateLimited = false, want true for a rate-limited attempt", entry)
			}
		case 200:
			if entry.RateLimited {
				t.Errorf("entry %+v has RateLimited = true, want false for a normal attempt", entry)
			}
		}
	}
}

// --- AC9: redaction happens before persistence ---

func TestRecord_RedactsTheRequestAndResponseBodiesBeforePersistence(t *testing.T) {
	f := newFacade()
	const rawToken = "raw-access-token-value"

	err := f.Record(context.Background(), recordInput(testOrg, func(in *logging.RecordInput) {
		in.RequestBody = `{"headers":{"Authorization":"Bearer ` + rawToken + `"}}`
		in.ResponseBody = `{"access_token":"` + rawToken + `","refresh_token":"raw-refresh"}`
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	entry := result.Entries[0]
	if strings.Contains(entry.RequestBody, rawToken) {
		t.Fatalf("persisted RequestBody %q contains the raw access token", entry.RequestBody)
	}
	if strings.Contains(entry.ResponseBody, rawToken) {
		t.Fatalf("persisted ResponseBody %q contains the raw access token", entry.ResponseBody)
	}
	if !strings.Contains(entry.RequestBody, logging.RedactedPlaceholder) {
		t.Errorf("persisted RequestBody %q does not carry the redaction marker", entry.RequestBody)
	}
	if !strings.Contains(entry.ResponseBody, logging.RedactedPlaceholder) {
		t.Errorf("persisted ResponseBody %q does not carry the redaction marker", entry.ResponseBody)
	}
}

// --- AC10: filters ---

func TestQuery_FiltersByConnectionID(t *testing.T) {
	f := newFacade()
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.ConnectionID = "conn_a" }))
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.ConnectionID = "conn_b" }))

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{ConnectionID: "conn_a"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	if result.Entries[0].ConnectionID != "conn_a" {
		t.Errorf("ConnectionID = %q, want %q", result.Entries[0].ConnectionID, "conn_a")
	}
}

func TestQuery_FiltersByUserID(t *testing.T) {
	f := newFacade()
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.UserID = "user_a" }))
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.UserID = "user_b" }))

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{UserID: "user_a"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].UserID != "user_a" {
		t.Fatalf("got %+v, want exactly one entry for user_a", result.Entries)
	}
}

func TestQuery_FiltersByToolSlug(t *testing.T) {
	f := newFacade()
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.ToolSlug = "outlook-list-messages" }))
	mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.ToolSlug = "outlook-get-message" }))

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{ToolSlug: "outlook-get-message"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 1 || result.Entries[0].ToolSlug != "outlook-get-message" {
		t.Fatalf("got %+v, want exactly one entry for outlook-get-message", result.Entries)
	}
}

func TestQuery_FiltersByFromAndToTimeRange(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	f := memory.NewFacadeWithOverrides(memory.Overrides{Now: func() time.Time { return clock }})

	clock = base.Add(-2 * time.Hour)
	mustRecord(t, f, recordInput(testOrg, nil)) // too early
	clock = base
	mustRecord(t, f, recordInput(testOrg, nil)) // in range
	clock = base.Add(2 * time.Hour)
	mustRecord(t, f, recordInput(testOrg, nil)) // too late

	from := base.Add(-30 * time.Minute)
	to := base.Add(30 * time.Minute)
	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{From: &from, To: &to})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want exactly 1 within [from, to]", len(result.Entries))
	}
	if !result.Entries[0].CreatedAt.Equal(base) {
		t.Errorf("CreatedAt = %v, want %v", result.Entries[0].CreatedAt, base)
	}
}

// --- AC10: cursor pagination ---

func TestQuery_CursorPaginationWalksNewestFirstWithoutDuplicatesOrGaps(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	f := memory.NewFacadeWithOverrides(memory.Overrides{Now: func() time.Time { return clock }})
	const total = 5
	for i := 0; i < total; i++ {
		clock = base.Add(time.Duration(i) * time.Minute)
		mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.Status = 200 + i }))
	}

	var seen []logging.EventLog
	cursor := ""
	for page := 0; page < total+1; page++ {
		result, err := f.Query(context.Background(), testOrg, logging.QueryParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("Query page %d: %v", page, err)
		}
		seen = append(seen, result.Entries...)
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("walked %d entries across all pages, want exactly %d (no duplicates or gaps)", len(seen), total)
	}
	ids := make(map[logging.LogID]bool, total)
	for i, entry := range seen {
		if ids[entry.ID] {
			t.Fatalf("entry id %q seen more than once while paginating", entry.ID)
		}
		ids[entry.ID] = true
		if i > 0 && !seen[i-1].CreatedAt.After(entry.CreatedAt) && seen[i-1].CreatedAt != entry.CreatedAt {
			t.Errorf("entries out of newest-first order at index %d: %v then %v", i, seen[i-1].CreatedAt, entry.CreatedAt)
		}
	}
	// Newest first: the very first entry across all pages must be the last one recorded.
	if seen[0].Status != 200+total-1 {
		t.Errorf("first entry status = %d, want %d (the most recently recorded entry)", seen[0].Status, 200+total-1)
	}
	if seen[len(seen)-1].Status != 200 {
		t.Errorf("last entry status = %d, want %d (the earliest recorded entry)", seen[len(seen)-1].Status, 200)
	}
}

func TestQuery_AnInvalidCursorReturnsAnError(t *testing.T) {
	f := newFacade()

	_, err := f.Query(context.Background(), testOrg, logging.QueryParams{Cursor: "not-valid-base64!!!"})

	if err == nil {
		t.Fatal("expected an error for a malformed cursor")
	}
}

// --- AC10: org isolation ---

func TestQuery_OrgBNeverSeesOrgAsEntries(t *testing.T) {
	f := newFacade()
	mustRecord(t, f, recordInput(testOrg, nil))
	mustRecord(t, f, recordInput(testOrg, nil))
	mustRecord(t, f, recordInput(otherOrg, nil))

	result, err := f.Query(context.Background(), otherOrg, logging.QueryParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("org B sees %d entries, want exactly its own 1", len(result.Entries))
	}
	for _, entry := range result.Entries {
		if entry.OrgID != otherOrg {
			t.Errorf("entry.OrgID = %q, want %q — org isolation violated", entry.OrgID, otherOrg)
		}
	}
}

func mustRecord(t *testing.T, f *logging.Facade, in logging.RecordInput) {
	t.Helper()
	if err := f.Record(context.Background(), in); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

// TestQuery_DefaultLimitCapsALargeResultSet is a lightweight boundary check
// on PD10's page-size bounds, using enough entries to actually observe the
// default (50) capping a large result set.
func TestQuery_DefaultLimitCapsALargeResultSet(t *testing.T) {
	f := newFacade()
	for i := 0; i < 60; i++ {
		mustRecord(t, f, recordInput(testOrg, func(in *logging.RecordInput) { in.Status = 200 + i%10 }))
	}

	result, err := f.Query(context.Background(), testOrg, logging.QueryParams{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Entries) != 50 {
		t.Fatalf("got %d entries, want the default page limit of 50", len(result.Entries))
	}
	if result.NextCursor == "" {
		t.Error("expected a NextCursor since more entries remain")
	}
}
