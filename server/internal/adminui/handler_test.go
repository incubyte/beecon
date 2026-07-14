package adminui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/adminui"
)

// newTestHandler builds the real Handler over whatever is actually embedded
// under server/internal/adminui/dist at test-build time — the committed
// placeholder index.html + .gitkeep on a checkout that hasn't run
// `make build-ui` (FD2), or a real Vite build's output otherwise. Either
// way, the fallback/asset-serving behavior under test here is identical.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := adminui.Handler()
	if err != nil {
		t.Fatalf("adminui.Handler(): unexpected error: %v", err)
	}
	return h
}

func getAdmin(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// TestHandler_ServesIndexHTMLAtTheAdminRoot pins that the mount actually
// serves a real embedded asset (not a 404) for the SPA's own entry point.
func TestHandler_ServesIndexHTMLAtTheAdminRoot(t *testing.T) {
	w := getAdmin(newTestHandler(t), "/admin/")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want a text/html response", ct)
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("body = %q, want it to contain the embedded index.html's markup", w.Body.String())
	}
}

// TestHandler_ServesARealEmbeddedNonIndexAssetByItsOwnPath proves the
// mount's "hit" branch: a path that names a real embedded file (here, the
// committed dist/.gitkeep placeholder — embedded via the "all:" go:embed
// directive specifically so dotfiles like it are included) is served
// as-is, distinct from the SPA-fallback branch every unmatched path takes.
func TestHandler_ServesARealEmbeddedNonIndexAssetByItsOwnPath(t *testing.T) {
	w := getAdmin(newTestHandler(t), "/admin/.gitkeep")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<html") {
		t.Errorf("body = %q, want the raw (empty) .gitkeep contents, not an index.html fallback", w.Body.String())
	}
}

// TestHandler_FallsBackToIndexHTMLForAnUnknownClientSideRoute is the SPA
// fallback (Slice 1, AC1): a path with no matching embedded file — exactly
// what a hard reload on a client-side route like /admin/organizations
// produces — renders the app shell in place (200 + index.html) instead of a
// 404 that would bounce the operator out of the console.
func TestHandler_FallsBackToIndexHTMLForAnUnknownClientSideRoute(t *testing.T) {
	w := getAdmin(newTestHandler(t), "/admin/organizations")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("body = %q, want the embedded index.html's markup as the SPA fallback", w.Body.String())
	}
}

// TestHandler_FallsBackForANestedUnknownClientSideRoute confirms the
// fallback applies at any depth under /admin, not just one path segment.
func TestHandler_FallsBackForANestedUnknownClientSideRoute(t *testing.T) {
	w := getAdmin(newTestHandler(t), "/admin/organizations/org_123/settings")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<html") {
		t.Errorf("body = %q, want the embedded index.html's markup as the SPA fallback", w.Body.String())
	}
}

// TestHandler_ResponseNeverRedirectsAwayFromTheRequestedRoute pins the
// deliberate deviation from net/http's own FileServer documented in
// handler.go: falling back to index.html must render its bytes directly at
// the originally requested path, never a 301 to "./" (which would drop a
// client-side route like /admin/organizations entirely).
func TestHandler_ResponseNeverRedirectsAwayFromTheRequestedRoute(t *testing.T) {
	w := getAdmin(newTestHandler(t), "/admin/organizations")

	if w.Code >= 300 && w.Code < 400 {
		t.Fatalf("status = %d, want no redirect for a client-side route fallback", w.Code)
	}
}
