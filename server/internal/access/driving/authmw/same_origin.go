package authmw

import (
	"net/http"
	"net/url"
	"strings"

	"beecon/internal/httpx"
)

// SameOriginOnly guards a pre-session route — POST /api/v1/auth/login is
// this slice's only caller — against cross-site forgery (FD-F, spec Slice 3
// AC5). Login has no session yet, so the double-submit X-CSRF-Token check
// above cannot apply there; instead this asserts the request itself came
// from the installation's own origin.
//
// Sec-Fetch-Site — the Fetch Metadata header every current browser attaches
// — is checked first: "cross-site" is rejected outright; any other value
// ("same-origin", "same-site", "none", i.e. the user typing the URL or
// following a bookmark) passes. Where a browser hasn't sent Sec-Fetch-Site,
// Origin is compared against baseURL's own origin instead: present and
// mismatched is rejected, present and matching passes.
//
// Deliberate decision (FD-F) for the case where BOTH headers are absent:
// the request is allowed through. Every current browser attaches Origin (and
// Sec-Fetch-Site) to a POST regardless of whether it is same- or cross-site
// — that is exactly the property this whole check relies on — so a request
// missing both headers did not come from a browser at all (e.g. a health
// check, an integration test, a CLI script calling the API directly with a
// real password). Such a caller cannot be the victim of login-CSRF (there is
// no browser session for a malicious page to hijack), so allowing it through
// is not a hole in the browser-facing defense this middleware exists for.
func SameOriginOnly(baseURL string) func(http.Handler) http.Handler {
	expectedOrigin := originOf(baseURL)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isSameOriginRequest(r, expectedOrigin) {
				httpx.WriteDomainError(w, httpx.Forbidden("cross-site request rejected"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isSameOriginRequest applies SameOriginOnly's own doc-commented decision
// tree to one request.
func isSameOriginRequest(r *http.Request, expectedOrigin string) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site != "cross-site"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return expectedOrigin != "" && strings.EqualFold(origin, expectedOrigin)
}

// originOf reduces baseURL (BEECON_BASE_URL) to its scheme+host origin, the
// same shape a browser's Origin header carries — a malformed baseURL (which
// config.Load already guards against at boot) resolves to "", which makes
// every Origin-carrying request fail closed rather than silently matching
// nothing.
func originOf(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
