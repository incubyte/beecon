//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header). This file tells Phase 5 Slice 3's story end to end
// against the real composition root: login -> a session-authenticated
// console mutation without the CSRF header is rejected -> the same mutation
// with a wrong token is rejected -> with the session's own token (read the
// same way the SPA's api-client reads it, off the beecon_csrf cookie) it
// succeeds -> a second, independent session's own token is rejected on the
// first session's cookie (bound to the session, not the operator) -> a
// session-authenticated safe read never needed a token at all -> logout
// itself is CSRF-protected the same way -> POST /auth/login's own
// same-origin defense (FD-F), including the deliberate both-headers-absent
// allow -> the pre-bootstrap admin-key Bearer branch stays exempt from CSRF
// (the Phase 4 regression this slice must never break).
package crucial_path

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/test/support"
)

// doCSRFGuardedRequest is doOperatorAuthRequest (operator_auth_bootstrap_and_
// login_journey_integration_test.go) plus an explicit X-CSRF-Token header,
// for exercising Slice 3's double-submit check directly rather than through
// the logout-only convenience csrfTokenFrom/doOperatorAuthLogoutRequest pair
// (operator_auth_logout_expiry_and_revocation_journey_integration_test.go).
func doCSRFGuardedRequest(t *testing.T, handler http.Handler, method, path, csrfHeader, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if csrfHeader != "" {
		req.Header.Set("X-CSRF-Token", csrfHeader)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// doLoginSameOriginRequest drives POST /api/v1/auth/login with the specific
// Sec-Fetch-Site/Origin combination SameOriginOnly (FD-F) branches on.
func doLoginSameOriginRequest(handler http.Handler, secFetchSite, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	if secFetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestOperatorAuthCSRFProtectionJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
		`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap fixture: status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	login := func(t *testing.T) []*http.Cookie {
		t.Helper()
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("login: status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		return w.Result().Cookies()
	}

	firstSessionCookies := login(t)
	firstSessionCSRFToken := csrfTokenFrom(firstSessionCookies)
	if firstSessionCSRFToken == "" {
		t.Fatal("test fixture bug: no beecon_csrf cookie value captured from login")
	}

	t.Run("a session-authenticated console mutation with no X-CSRF-Token header is rejected", func(t *testing.T) {
		w := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", "", `{"name":"Acme"}`, firstSessionCookies)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error body: %v; body=%s", err, w.Body.String())
		}
		if env.Error.Code != "csrf_failed" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "csrf_failed")
		}
		if strings.Contains(w.Body.String(), firstSessionCSRFToken) {
			t.Errorf("the rejection body must never contain the session's own CSRF token, got: %s", w.Body.String())
		}
	})

	t.Run("the same console mutation with a wrong X-CSRF-Token header is rejected", func(t *testing.T) {
		w := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", "not-the-right-token", `{"name":"Acme"}`, firstSessionCookies)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	var createdOrg organizationDTO
	t.Run("the same console mutation with the session's own CSRF token (read off the beecon_csrf cookie, exactly as the SPA's api-client does) succeeds", func(t *testing.T) {
		w := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", firstSessionCSRFToken, `{"name":"Acme"}`, firstSessionCookies)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &createdOrg); err != nil {
			t.Fatalf("decode create response: %v; body=%s", err, w.Body.String())
		}
		if createdOrg.Name != "Acme" {
			t.Errorf("name = %q, want %q", createdOrg.Name, "Acme")
		}
	})

	t.Run("a session-authenticated safe read (GET) never needed a CSRF token at all", func(t *testing.T) {
		w := doCSRFGuardedRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/", "", "", firstSessionCookies)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("a CSRF token minted for a second, independent session is rejected on the first session's own cookie", func(t *testing.T) {
		secondSessionCookies := login(t)
		secondSessionCSRFToken := csrfTokenFrom(secondSessionCookies)
		if secondSessionCSRFToken == "" {
			t.Fatal("test fixture bug: no beecon_csrf cookie value captured from the second login")
		}
		if secondSessionCSRFToken == firstSessionCSRFToken {
			t.Fatal("test fixture bug: the second login minted the same CSRF token as the first — sessions aren't actually independent in this run")
		}

		w := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", secondSessionCSRFToken, `{"name":"Globex"}`, firstSessionCookies)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d — a CSRF token bound to a different session must never satisfy this session's check; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}

		// The second session's own token, on its own cookie, still works —
		// confirming the rejection above is really about cross-session
		// binding, not some other fixture bug (e.g. a stale/expired token).
		wOwnToken := doCSRFGuardedRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", secondSessionCookies)
		if wOwnToken.Code != http.StatusOK {
			t.Fatalf("second session's own GET /auth/me: status = %d, want %d", wOwnToken.Code, http.StatusOK)
		}
	})

	t.Run("logout without the CSRF token is rejected, and with it succeeds", func(t *testing.T) {
		logoutSessionCookies := login(t)
		logoutCSRFToken := csrfTokenFrom(logoutSessionCookies)

		withoutToken := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/logout", "", "", logoutSessionCookies)
		if withoutToken.Code != http.StatusForbidden {
			t.Fatalf("logout without X-CSRF-Token: status = %d, want %d; body=%s", withoutToken.Code, http.StatusForbidden, withoutToken.Body.String())
		}

		withToken := doCSRFGuardedRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/logout", logoutCSRFToken, "", logoutSessionCookies)
		if withToken.Code != http.StatusNoContent {
			t.Fatalf("logout with the correct X-CSRF-Token: status = %d, want %d; body=%s", withToken.Code, http.StatusNoContent, withToken.Body.String())
		}
	})

	// --- POST /api/v1/auth/login's own same-origin defense (FD-F). ---

	t.Run("login is rejected when Sec-Fetch-Site is cross-site", func(t *testing.T) {
		w := doLoginSameOriginRequest(wired.Router, "cross-site", "", `{"email":"founder@example.com","password":"correct horse battery staple"}`)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("login is rejected when Origin is present and mismatched (no Sec-Fetch-Site)", func(t *testing.T) {
		w := doLoginSameOriginRequest(wired.Router, "", "https://evil.example.com", `{"email":"founder@example.com","password":"correct horse battery staple"}`)

		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("login succeeds when Origin matches the installation's own base URL (no Sec-Fetch-Site)", func(t *testing.T) {
		w := doLoginSameOriginRequest(wired.Router, "", "http://localhost:8080", `{"email":"founder@example.com","password":"correct horse battery staple"}`)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
	})

	t.Run("login succeeds when Sec-Fetch-Site is same-origin", func(t *testing.T) {
		w := doLoginSameOriginRequest(wired.Router, "same-origin", "", `{"email":"founder@example.com","password":"correct horse battery staple"}`)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
	})

	// This pins FD-F's deliberate decision, at the real composition root, not
	// just the isolated middleware unit test: a caller presenting neither
	// header at all (not a browser — e.g. a CLI script with the real
	// password) is allowed through login's same-origin gate.
	t.Run("login succeeds when both Sec-Fetch-Site and Origin are absent (FD-F's deliberate non-browser-caller allowance)", func(t *testing.T) {
		w := doLoginSameOriginRequest(wired.Router, "", "", `{"email":"founder@example.com","password":"correct horse battery staple"}`)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d — a caller with neither header cannot be a browser, and so cannot be a login-CSRF victim; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
	})

	// --- Regression: the Phase 4 Bearer-authenticated console journeys stay
	// green — the admin-key branch is never CSRF-checked (a fresh,
	// pre-bootstrap app, since the shared `wired` app above already has an
	// operator and would 401 the admin key on a general route regardless of
	// CSRF). ---

	t.Run("the pre-bootstrap admin-key Bearer branch accepts a mutating request with no X-CSRF-Token at all", func(t *testing.T) {
		freshWired := support.BootApp(t)

		w := doOperatorAuthRequest(t, freshWired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`, nil)

		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d — the admin-key Bearer branch is not cookie-borne and must never be CSRF-checked; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})
}
