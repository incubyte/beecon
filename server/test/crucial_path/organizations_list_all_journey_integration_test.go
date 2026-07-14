//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go's own
// doc comment). This file tells Slice 1's headline "list every organization
// in the installation" story end to end against the real composition root
// (real chi router, real SQLite-backed bun repository) — the router-level
// test in server/internal/app exercises the identical buildRouter wiring
// with the in-memory facade; this one additionally proves the real
// SQL-backed ListAll (ordering, cursor) serializes through the handler
// exactly the same way.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"beecon/test/support"
)

type organizationsPageDTO struct {
	Items      []organizationDTO `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// TestOrganizationsListAllJourney: boot -> list-without-key rejected ->
// list-with-wrong-key rejected -> create three organizations -> list
// returns all three, newest first -> a small page size plus its returned
// nextCursor resumes exactly where the first page left off.
//
// The app boots with a MovableClock advanced by a full second between each
// create rather than the real wall clock: CUID2 ids are deliberately not
// time-sortable (context.md), so two organizations created back-to-back on
// the real clock can land in the same created_at instant (or even the same
// stored-precision bucket) and then tiebreak on id — which has no relation
// to creation order — making "the last-created organization sorts first"
// nondeterministic on a real clock. The movable clock is the same
// determinism tool the facade- and bun-level ListAll tests already use.
func TestOrganizationsListAllJourney(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, nil, clock.Now)
	authHeader := "Bearer " + support.AdminAPIKey

	t.Run("listing organizations without the admin key is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("listing organizations with a wrong admin key is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", "Bearer wrong-key", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	var created []organizationDTO
	for _, name := range []string{"Acme", "Globex", "Initech"} {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", authHeader, `{"name":"`+name+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q status = %d, want %d; body=%s", name, w.Code, http.StatusCreated, w.Body.String())
		}
		var org organizationDTO
		if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		created = append(created, org)
		clock.Advance(time.Second)
	}

	t.Run("listing returns every created organization, newest first", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", authHeader, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var page organizationsPageDTO
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
		}
		if len(page.Items) != 3 {
			t.Fatalf("got %d items, want 3", len(page.Items))
		}
		// created[2] ("Initech") was created last, so it must sort first.
		if page.Items[0].ID != created[2].ID {
			t.Errorf("first item id = %q, want %q (the most recently created organization)", page.Items[0].ID, created[2].ID)
		}
		if page.NextCursor != "" {
			t.Errorf("nextCursor = %q, want empty on a single, complete page", page.NextCursor)
		}
	})

	t.Run("a small page size's nextCursor resumes exactly where the first page left off", func(t *testing.T) {
		firstPageResp := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations?limit=2", authHeader, "")
		if firstPageResp.Code != http.StatusOK {
			t.Fatalf("first page status = %d, want %d; body=%s", firstPageResp.Code, http.StatusOK, firstPageResp.Body.String())
		}
		var firstPage organizationsPageDTO
		if err := json.Unmarshal(firstPageResp.Body.Bytes(), &firstPage); err != nil {
			t.Fatalf("decode first page: %v", err)
		}
		if len(firstPage.Items) != 2 {
			t.Fatalf("first page has %d items, want 2", len(firstPage.Items))
		}
		if firstPage.NextCursor == "" {
			t.Fatal("first page's nextCursor is empty, want a cursor for the remaining organization")
		}

		secondPageResp := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations?limit=2&cursor="+firstPage.NextCursor, authHeader, "")
		if secondPageResp.Code != http.StatusOK {
			t.Fatalf("second page status = %d, want %d; body=%s", secondPageResp.Code, http.StatusOK, secondPageResp.Body.String())
		}
		var secondPage organizationsPageDTO
		if err := json.Unmarshal(secondPageResp.Body.Bytes(), &secondPage); err != nil {
			t.Fatalf("decode second page: %v", err)
		}
		if len(secondPage.Items) != 1 {
			t.Fatalf("second page has %d items, want 1 (the remaining organization)", len(secondPage.Items))
		}
		if secondPage.NextCursor != "" {
			t.Errorf("second page's nextCursor = %q, want empty (it was the last page)", secondPage.NextCursor)
		}
		for _, item := range secondPage.Items {
			for _, seen := range firstPage.Items {
				if item.ID == seen.ID {
					t.Errorf("id %q appeared on both pages, want each organization exactly once", item.ID)
				}
			}
		}
	})
}
