// tool_id_dto_test.go (package httpapi, reuses newTestRouter/mustRecordEntry/
// doRequestAsOrg/testOrg from handler_test.go) pins the Phase 5 registry
// sub-phase's Slice 5 wire shape: GET /api/v1/logs surfaces a tool-execution
// entry's immutable tool_ id under toolId, alongside its toolSlug, and omits
// it entirely for an entry recorded with none.
package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/logging"
)

func TestList_SurfacesTheExecutedToolsImmutableToolIDAlongsideItsSlug(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, func(in *logging.RecordInput) {
		in.ToolID = "tool_attribution_check"
		in.ToolSlug = "outlook-list-messages"
	})

	w := doRequestAsOrg(r, testOrg, "/")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(page.Entries))
	}
	if page.Entries[0].ToolID != "tool_attribution_check" {
		t.Errorf("toolId = %q, want %q", page.Entries[0].ToolID, "tool_attribution_check")
	}
	if page.Entries[0].ToolSlug != "outlook-list-messages" {
		t.Errorf("toolSlug = %q, want %q", page.Entries[0].ToolSlug, "outlook-list-messages")
	}
}

func TestList_OmitsToolIDForAnEntryRecordedWithNone(t *testing.T) {
	r, facade := newTestRouter(t)
	mustRecordEntry(t, facade, testOrg, nil) // the default fixture's RecordInput carries no ToolID

	w := doRequestAsOrg(r, testOrg, "/")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(page.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(page.Entries))
	}
	if page.Entries[0].ToolID != "" {
		t.Errorf("toolId = %q, want empty for an entry recorded with none", page.Entries[0].ToolID)
	}
}
