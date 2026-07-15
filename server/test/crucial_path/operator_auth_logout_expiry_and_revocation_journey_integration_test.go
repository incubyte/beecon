//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header). This file tells Phase 5 Slice 2's story end to end
// against the real composition root: a session is a revocable server-side
// fact, not just a cookie. login -> the session cookie authenticates a
// general console read -> logout answers 204 and clears both cookies ->
// the exact same (never-changed) cookie value is rejected on the very next
// request -> a repeated logout call against that now-revoked/absent session
// is rejected cleanly by authmw.OperatorSession, never a 500 (Logout
// handler's own 204-idempotent contract for a revoked/unknown/absent token
// is unit-tested directly, bypassing the middleware, in
// operator_handler_test.go — the same convention router.go's own doc comment
// on /auth/logout documents: production never lets a request without a
// still-valid session reach the handler at all) -> logging in again works ->
// traveling the clock past BEECON_SESSION_TTL (no real sleep) rejects that
// second session too. A real database read after logout confirms revoked_at
// was actually written, not just that the HTTP response looked right.
package crucial_path

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"beecon/internal/access/driving/authmw"
	"beecon/test/support"
)

// csrfTokenFrom reads the beecon_csrf cookie's value out of a login
// response's cookies (Slice 3, PD52): logout is a session-authenticated
// mutating call, so — now that authmw.OperatorSession's CSRF branch is
// active — it must echo this value back as the X-CSRF-Token header exactly
// as the SPA's api-client does.
func csrfTokenFrom(cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Name == authmw.CSRFCookieName {
			return c.Value
		}
	}
	return ""
}

// doOperatorAuthLogoutRequest is doOperatorAuthRequest for POST /auth/logout
// specifically: it carries the session cookie plus the matching
// X-CSRF-Token header (Slice 3), since logout is a session-authenticated
// mutating call and authmw.OperatorSession now enforces the double-submit
// check on it. When cookies carry no beecon_csrf value (e.g. the "no cookie
// at all" case below), no header is sent — the middleware rejects that
// request on the missing session itself, before it would ever reach the
// CSRF check.
func doOperatorAuthLogoutRequest(t *testing.T, handler http.Handler, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if csrfToken := csrfTokenFrom(cookies); csrfToken != "" {
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestOperatorAuthLogoutExpiryAndRevocationJourney(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, nil, clock.Now)
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

	var firstSessionCookies []*http.Cookie
	t.Run("login -> the session cookie authenticates a general console read", func(t *testing.T) {
		firstSessionCookies = login(t)

		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", "", "", firstSessionCookies)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("logout returns 204 and clears both PD52 cookies", func(t *testing.T) {
		w := doOperatorAuthLogoutRequest(t, wired.Router, firstSessionCookies)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		var sessionCleared, csrfCleared bool
		for _, c := range w.Result().Cookies() {
			switch c.Name {
			case "beecon_session":
				sessionCleared = c.Value == "" && c.MaxAge < 0
			case "beecon_csrf":
				csrfCleared = c.Value == "" && c.MaxAge < 0
			}
		}
		if !sessionCleared {
			t.Error("expected the beecon_session cookie to be cleared (empty value, Max-Age=0)")
		}
		if !csrfCleared {
			t.Error("expected the beecon_csrf cookie to be cleared (empty value, Max-Age=0)")
		}
	})

	// This is the crucial security assertion: the raw cookie value is
	// unchanged from the login subtest above — a copied/replayed token, not
	// just "the browser forgot the cookie" — and it must now be rejected.
	t.Run("the exact same (unchanged) session cookie is rejected as unauthenticated on the very next request", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", firstSessionCookies)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — a revoked session must never be resurrected by replaying its old cookie", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("a real database read confirms revoked_at was actually written for that session", func(t *testing.T) {
		var revokedAtIsNull bool
		row := wired.DB.QueryRowContext(context.Background(),
			"SELECT revoked_at IS NULL FROM operator_sessions")
		if err := row.Scan(&revokedAtIsNull); err != nil {
			t.Fatalf("query operator_sessions.revoked_at: %v", err)
		}
		if revokedAtIsNull {
			t.Error("expected revoked_at to be set on the logged-out session, got NULL")
		}
	})

	// authmw.OperatorSession guards /auth/logout itself (router.go): a
	// request that carries no still-valid session never reaches the Logout
	// handler's own idempotency in production at all, whether the cookie is
	// missing entirely or replays the session just revoked above — either
	// way, the crucial invariant this subtest pins is that a repeated call
	// is rejected cleanly (never a 500), matching the AC's "not a 500"
	// requirement even at this outer layer.
	t.Run("a repeated logout call — no cookie at all, or the just-revoked one replayed — is rejected cleanly, never a 500", func(t *testing.T) {
		noCookie := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/logout", "", "", nil)
		if noCookie.Code == http.StatusInternalServerError {
			t.Fatalf("logout with no session cookie: status = %d, want anything but 500", noCookie.Code)
		}
		if noCookie.Code != http.StatusUnauthorized {
			t.Fatalf("logout with no session cookie: status = %d, want %d", noCookie.Code, http.StatusUnauthorized)
		}

		replayed := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/logout", "", "", firstSessionCookies)
		if replayed.Code == http.StatusInternalServerError {
			t.Fatalf("logout replaying the already-revoked session cookie: status = %d, want anything but 500", replayed.Code)
		}
		if replayed.Code != http.StatusUnauthorized {
			t.Fatalf("logout replaying the already-revoked session cookie: status = %d, want %d", replayed.Code, http.StatusUnauthorized)
		}
	})

	var secondSessionCookies []*http.Cookie
	t.Run("logging in again succeeds and mints a fresh, independent session", func(t *testing.T) {
		secondSessionCookies = login(t)

		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", secondSessionCookies)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("traveling the clock past BEECON_SESSION_TTL rejects the second session too, with no real sleep", func(t *testing.T) {
		clock.Advance(13 * time.Hour) // default BEECON_SESSION_TTL is 12h

		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", secondSessionCookies)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — a session past its absolute expiry must be rejected even though the cookie is still present", w.Code, http.StatusUnauthorized)
		}
	})
}
