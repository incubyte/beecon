// Package httpapi (in-package test) exercises the logging module's List
// handler through an actual chi router, backed by the driven/memory facade —
// AC10's filters, cursor pagination, and org isolation, plus AC9's redacted
// bodies surfacing through the JSON response.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/logging"
	memory "beecon/internal/logging/driven/memory"
	"beecon/internal/organizations"
)

const (
	testOrg  = organizations.OrgID("org_1")
	otherOrg = organizations.OrgID("org_2")
)

func newTestRouter(t *testing.T) (chi.Router, *logging.Facade) {
	t.Helper()
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(nil)
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Get("/", h.List)
	return r, facade
}

func doRequestAsOrg(r chi.Router, org organizations.OrgID, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mustRecordEntry(t *testing.T, facade *logging.Facade, org organizations.OrgID, overrides func(*logging.RecordInput)) {
	t.Helper()
	in := logging.RecordInput{
		OrgID:        org,
		UserID:       "user_1",
		ConnectionID: "conn_1",
		ToolSlug:     "outlook-list-messages",
		Kind:         logging.KindToolExecution,
		Status:       200,
		DurationMs:   10,
	}
	if overrides != nil {
		overrides(&in)
	}
	if err := facade.Record(context.Background(), in); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

func TestList_Returns401WhenNoOrgContext(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doRequestAsOrg(r, "", "/")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestList_Returns200WithEveryEntryScopedToTheCallersOrganization(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, nil)
	mustRecordEntry(t, facade, otherOrg, nil)

	w := doRequestAsOrg(r, testOrg, "/")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 {
		t.Fatalf("got %d entries, want exactly the caller's own 1", len(page.Entries))
	}
	if page.Entries[0].OrgID != string(testOrg) {
		t.Errorf("organizationId = %q, want %q — org isolation violated", page.Entries[0].OrgID, testOrg)
	}
}

func TestList_FiltersByConnectionIDQueryParam(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.ConnectionID = "conn_a" })
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.ConnectionID = "conn_b" })

	w := doRequestAsOrg(r, testOrg, "/?connectionId=conn_a")

	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 || page.Entries[0].ConnectionID != "conn_a" {
		t.Fatalf("got %+v, want exactly one entry for connectionId=conn_a", page.Entries)
	}
}

func TestList_FiltersByUserIDQueryParam(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.UserID = "user_a" })
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.UserID = "user_b" })

	w := doRequestAsOrg(r, testOrg, "/?userId=user_a")

	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 || page.Entries[0].UserID != "user_a" {
		t.Fatalf("got %+v, want exactly one entry for userId=user_a", page.Entries)
	}
}

func TestList_FiltersByToolSlugQueryParam(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.ToolSlug = "outlook-list-messages" })
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.ToolSlug = "outlook-get-message" })

	w := doRequestAsOrg(r, testOrg, "/?toolSlug=outlook-get-message")

	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 || page.Entries[0].ToolSlug != "outlook-get-message" {
		t.Fatalf("got %+v, want exactly one entry for toolSlug=outlook-get-message", page.Entries)
	}
}

func TestList_FiltersByFromAndToQueryParams(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, nil)

	w := doRequestAsOrg(r, testOrg, "/?from=2099-01-01T00:00:00Z&to=2099-12-31T00:00:00Z")

	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 0 {
		t.Fatalf("got %d entries for a future time range, want 0", len(page.Entries))
	}
}

func TestList_Returns422ForAMalformedFromParam(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doRequestAsOrg(r, testOrg, "/?from=not-a-timestamp")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestList_Returns422ForAMalformedCursor(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doRequestAsOrg(r, testOrg, "/?cursor=not-valid-base64!!!")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestList_CursorPaginationWalksToASecondPageWithoutRepeatingEntries(t *testing.T) {
	r, facade := newTestRouter(t)
	for i := 0; i < 3; i++ {
		mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) { in.Status = 200 + i })
	}

	first := doRequestAsOrg(r, testOrg, "/?limit=2")
	var firstPage logsPageDTO
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Entries) != 2 {
		t.Fatalf("first page has %d entries, want 2", len(firstPage.Entries))
	}
	if firstPage.NextCursor == "" {
		t.Fatal("expected a nextCursor since a third entry remains")
	}

	second := doRequestAsOrg(r, testOrg, "/?limit=2&cursor="+firstPage.NextCursor)
	var secondPage logsPageDTO
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage.Entries) != 1 {
		t.Fatalf("second page has %d entries, want the remaining 1", len(secondPage.Entries))
	}
	if secondPage.Entries[0].ID == firstPage.Entries[0].ID || secondPage.Entries[0].ID == firstPage.Entries[1].ID {
		t.Errorf("second page entry %q duplicates one already seen on the first page", secondPage.Entries[0].ID)
	}
}

// TestList_RedactedBodiesSurfaceThroughTheAPIResponseWithoutTheRawSecret is
// AC9 at the HTTP boundary: Record already redacted the body before
// persistence, so the JSON response must carry the marker, never the raw
// value.
func TestList_RedactedBodiesSurfaceThroughTheAPIResponseWithoutTheRawSecret(t *testing.T) {
	r, facade := newTestRouter(t)
	const rawToken = "raw-microsoft-access-token-value"
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) {
		in.RequestBody = `{"headers":{"Authorization":"Bearer ` + rawToken + `"}}`
		in.ResponseBody = `{"access_token":"` + rawToken + `"}`
	})

	w := doRequestAsOrg(r, testOrg, "/")

	body := w.Body.String()
	if strings.Contains(body, rawToken) {
		t.Fatalf("logs API response contains the raw access token: %s", body)
	}
	if !strings.Contains(body, logging.RedactedPlaceholder) {
		t.Errorf("logs API response does not carry the redaction marker: %s", body)
	}
}
