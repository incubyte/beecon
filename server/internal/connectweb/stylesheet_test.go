// Package connectweb_test — this file covers Slice 10's shared stylesheet
// (PD48): GET /connect/style.css serves the design-token CSS the three
// connect templates now link via `<style>@import url("/connect/style.css")`
// instead of each carrying its own duplicated inline block, and the a11y
// rules the design brief requires (`:focus-visible`, 44px targets,
// `prefers-reduced-motion`). Reuses newTestFixture, fakeOAuthClient,
// testIntegration, and testParamsIntegration declared in handler_test.go.
package connectweb_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/connectweb"
)

// getStylesheet mounts only the Stylesheet handler behind a bare chi router —
// isolated from the connect/params/error fixture in handler_test.go since
// serving the stylesheet needs no organization/integration/user scaffolding.
func getStylesheet(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := connectweb.NewHandler(memory.NewFacadeWithOverrides(memory.Overrides{}))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	r := chi.NewRouter()
	r.Get("/connect/style.css", handler.Stylesheet)

	req := httptest.NewRequest(http.MethodGet, "/connect/style.css", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestStylesheet_ServesTextCSSWith200StatusAndACacheHeader(t *testing.T) {
	w := getStylesheet(t)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want a text/css content type", ct)
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Error("Cache-Control header is empty, want a cache directive present")
	}
	if w.Body.Len() == 0 {
		t.Fatal("stylesheet body is empty")
	}
}

func TestStylesheet_BodyCarriesTheSharedConnectPrimaryDesignToken(t *testing.T) {
	body := getStylesheet(t).Body.String()

	if !strings.Contains(body, "--connect-primary") || !strings.Contains(body, "#2563eb") {
		t.Errorf("stylesheet body does not carry the --connect-primary #2563eb design token: %s", body)
	}
}

func TestStylesheet_BodyCarriesFocusVisibleRules(t *testing.T) {
	body := getStylesheet(t).Body.String()

	if !strings.Contains(body, ":focus-visible") {
		t.Errorf("stylesheet body does not carry a :focus-visible rule: %s", body)
	}
}

func TestStylesheet_BodyCarriesMinimum44pxTouchTargetSizing(t *testing.T) {
	body := getStylesheet(t).Body.String()

	if !strings.Contains(body, "44px") {
		t.Errorf("stylesheet body does not carry a 44px minimum touch-target rule: %s", body)
	}
}

func TestStylesheet_BodyCarriesAPrefersReducedMotionOverride(t *testing.T) {
	body := getStylesheet(t).Body.String()

	if !strings.Contains(body, "prefers-reduced-motion") {
		t.Errorf("stylesheet body does not carry a prefers-reduced-motion override: %s", body)
	}
}

// --- Behavior preservation: the dedup actually happened (Slice 10) ---

// assertReferencesSharedStylesheetWithNoDuplicatedInlineCSS is shared by the
// three page-rendering tests below (third-occurrence extraction): each
// rendered page must link the shared stylesheet via @import and must no
// longer carry its own copy of the old duplicated :root/.card/.connect-button
// rule block.
func assertReferencesSharedStylesheetWithNoDuplicatedInlineCSS(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, `@import url("/connect/style.css")`) {
		t.Errorf("body does not reference the shared stylesheet via @import: %s", body)
	}
	if strings.Contains(body, "color-scheme: light") {
		t.Errorf("body still carries the old duplicated inline :root { color-scheme: light } rule — dedup did not happen: %s", body)
	}
	if strings.Contains(body, ".connect-button {") {
		t.Errorf("body still carries a duplicated inline .connect-button rule block — dedup did not happen: %s", body)
	}
	if n := strings.Count(body, "<style"); n > 1 {
		t.Errorf("body carries %d <style> blocks, want exactly the single @import block: %s", n, body)
	}
}

func TestConnectPage_ReferencesTheSharedStylesheetAndCarriesNoDuplicatedInlineCSS(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiate(t)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	body := f.getConnectPage(token).Body.String()

	assertReferencesSharedStylesheetWithNoDuplicatedInlineCSS(t, body)
}

func TestParamsForm_ReferencesTheSharedStylesheetAndCarriesNoDuplicatedInlineCSS(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})
	initiated := f.initiateForIntegration(t, testParamsIntegration)
	token := strings.TrimPrefix(initiated.RedirectURL, "http://localhost:8080/connect/")

	body := f.getConnectPage(token).Body.String()

	assertReferencesSharedStylesheetWithNoDuplicatedInlineCSS(t, body)
}

func TestErrorPage_ReferencesTheSharedStylesheetAndCarriesNoDuplicatedInlineCSS(t *testing.T) {
	f := newTestFixture(t, &fakeOAuthClient{})

	body := f.getConnectPage("token-that-does-not-exist").Body.String()

	assertReferencesSharedStylesheetWithNoDuplicatedInlineCSS(t, body)
}
