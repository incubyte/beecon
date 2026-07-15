// Package authmw_test (see admin_test.go's own header for the doRequest/
// wireErrorEnvelope conventions reused here). This file covers Phase 5
// Slice 3's SameOriginOnly middleware (FD-F, architecture doc §3/§4.2): the
// pre-session cross-site defense for POST /api/v1/auth/login, since login has
// no session yet for the double-submit CSRF check to bind to.
package authmw_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/access/driving/authmw"
)

// sameOriginTestBaseURL is the installation's own BEECON_BASE_URL for these
// tests — the origin SameOriginOnly compares an absent-Sec-Fetch-Site
// request's Origin header against.
const sameOriginTestBaseURL = "https://op.example.com"

func newSameOriginHandler(baseURL string) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("passed"))
	})
	return authmw.SameOriginOnly(baseURL)(next)
}

func doSameOriginRequest(h http.Handler, secFetchSite, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	if secFetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// --- Sec-Fetch-Site present: checked first, decides the outcome alone. ---

func TestSameOriginOnly_RejectsACrossSiteRequestViaSecFetchSite(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "cross-site", "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestSameOriginOnly_AllowsASameOriginRequestViaSecFetchSite(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "same-origin", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestSameOriginOnly_AllowsASameSiteRequestViaSecFetchSite(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "same-site", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestSameOriginOnly_AllowsANoneSecFetchSiteValue pins "none" (the user typed
// the URL or followed a bookmark) as a pass, per SameOriginOnly's own doc
// comment.
func TestSameOriginOnly_AllowsANoneSecFetchSiteValue(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "none", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestSameOriginOnly_SecFetchSiteTakesPriorityOverAMismatchedOrigin pins the
// decision tree's order: when Sec-Fetch-Site is present, Origin is never
// consulted at all, even if it would otherwise fail the check.
func TestSameOriginOnly_SecFetchSiteTakesPriorityOverAMismatchedOrigin(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "same-origin", "https://evil.example.com")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — a present Sec-Fetch-Site must decide the outcome alone", w.Code, http.StatusOK)
	}
}

// --- Sec-Fetch-Site absent: falls back to comparing Origin. ---

func TestSameOriginOnly_RejectsAMismatchedOriginWhenSecFetchSiteIsAbsent(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "", "https://evil.example.com")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestSameOriginOnly_AllowsAMatchingOriginWhenSecFetchSiteIsAbsent(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "", sameOriginTestBaseURL)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestSameOriginOnly_OriginComparisonIsCaseInsensitiveOnScheme(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "", "HTTPS://OP.EXAMPLE.COM")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — the origin comparison must be case-insensitive", w.Code, http.StatusOK)
	}
}

// TestSameOriginOnly_AllowsARequestWithBothHeadersAbsent pins FD-F's own
// deliberate decision (SameOriginOnly's doc comment): every current browser
// attaches Origin (and usually Sec-Fetch-Site) to a POST regardless of
// same- or cross-site, so a request missing both did not come from a browser
// at all and is allowed through — a future change to this must be a
// conscious one, not a silent regression this test would catch.
func TestSameOriginOnly_AllowsARequestWithBothHeadersAbsent(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — a caller with neither header is not a browser, and cannot be a login-CSRF victim (FD-F)", w.Code, http.StatusOK)
	}
}

// --- Rejection shape. ---

func TestSameOriginOnly_ACrossSiteRejectionRendersTheForbiddenEnvelope(t *testing.T) {
	h := newSameOriginHandler(sameOriginTestBaseURL)

	w := doSameOriginRequest(h, "cross-site", "")

	env := decodeWireError(t, w.Body.Bytes())
	if env.Error.Code != "forbidden" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "forbidden")
	}
}

// TestSameOriginOnly_AMalformedBaseURLFailsClosedRatherThanMatchingEveryOrigin
// pins originOf's own doc comment: an unparseable baseURL (config.Load
// already guards against this at boot) reduces to "", so an Origin-carrying
// request never matches nothing — it always fails closed.
func TestSameOriginOnly_AMalformedBaseURLFailsClosedRatherThanMatchingEveryOrigin(t *testing.T) {
	h := newSameOriginHandler("not-a-valid-url")

	w := doSameOriginRequest(h, "", "https://op.example.com")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d — a malformed base URL must never make every Origin match", w.Code, http.StatusForbidden)
	}
}
