//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header). This file tells Phase 5 Slice 1's walking-skeleton story
// end to end against the real composition root: the admin key still opens
// the console before any operator exists -> bootstrap the first operator ->
// the admin key is demoted the instant that operator exists (PD54, Slice 4's
// AC8 wired from Slice 1) -> log in -> the session cookie authenticates both
// /auth/me and a general console read -> a second bootstrap is rejected ->
// the session row a real database dump produces never carries the raw
// opaque token, only its SHA-256 hash.
package crucial_path

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/test/support"
)

type bootstrappedOperatorDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type sessionOperatorDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// doOperatorAuthRequest is doJSONRequest plus the ability to ride an
// already-issued session cookie — organizations_journey_integration_test.go's
// own doJSONRequest never attaches cookies, since no route needed one before
// Phase 5.
func doOperatorAuthRequest(t *testing.T, handler http.Handler, method, path, authHeader, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestOperatorAuthBootstrapAndLoginJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	t.Run("before any operator exists, the admin key alone still opens a general console route", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", adminAuth, "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (pre-bootstrap break-glass window); body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	var bootstrapped bootstrappedOperatorDTO
	t.Run("bootstrapping the first operator with the admin key succeeds", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
			`{"email":"Founder@Example.com","password":"correct horse battery staple"}`, nil)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &bootstrapped); err != nil {
			t.Fatalf("decode bootstrapped operator: %v; body=%s", err, w.Body.String())
		}
		if bootstrapped.Email != "founder@example.com" {
			t.Errorf("email = %q, want lowercased %q", bootstrapped.Email, "founder@example.com")
		}
		if !strings.HasPrefix(bootstrapped.ID, "op_") {
			t.Errorf("id = %q, want it to start with %q", bootstrapped.ID, "op_")
		}
	})

	t.Run("bootstrapping a second operator is rejected — bootstrap is first-account-only", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
			`{"email":"someone-else@example.com","password":"correct horse battery staple"}`, nil)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
		}
	})

	// This is the direct AC8/PD54 live-demotion assertion: the identical
	// admin key that opened the console in the very first subtest above is
	// now rejected on the same route, with no restart in between — the
	// operatorsExist predicate is re-checked on every request, not cached.
	t.Run("once an operator account exists, the same admin key no longer opens the general console", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", adminAuth, "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — the admin key must be demoted the instant an operator account exists", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("logging in with a wrong password is rejected generically", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"totally-wrong-password"}`, nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error body: %v; body=%s", err, w.Body.String())
		}
		if env.Error.Message != "invalid credentials" {
			t.Errorf("error.message = %q, want the generic %q", env.Error.Message, "invalid credentials")
		}
	})

	t.Run("logging in with an unknown email is rejected with the byte-identical generic error", func(t *testing.T) {
		wrongPassword := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"totally-wrong-password"}`, nil)
		unknownEmail := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"nobody-has-this-account@example.com","password":"correct horse battery staple"}`, nil)

		if wrongPassword.Code != unknownEmail.Code {
			t.Fatalf("wrong-password status %d != unknown-email status %d", wrongPassword.Code, unknownEmail.Code)
		}
		if wrongPassword.Body.String() != unknownEmail.Body.String() {
			t.Fatalf("response bodies differ: wrong-password=%s unknown-email=%s — the caller must not be able to tell which case occurred", wrongPassword.Body.String(), unknownEmail.Body.String())
		}
	})

	var sessionCookies []*http.Cookie
	t.Run("logging in with the correct email and password sets HttpOnly/Secure-per-flag/SameSite=Strict session and CSRF cookies", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"Founder@Example.com","password":"correct horse battery staple"}`, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		if w.Body.Len() != 0 {
			t.Errorf("expected an empty 204 body, got %q — the opaque session token must never appear in the response body", w.Body.String())
		}
		sessionCookies = w.Result().Cookies()
		var session, csrf *http.Cookie
		for _, c := range sessionCookies {
			switch c.Name {
			case "beecon_session":
				session = c
			case "beecon_csrf":
				csrf = c
			}
		}
		if session == nil {
			t.Fatal("expected a beecon_session cookie to be set")
		}
		if csrf == nil {
			t.Fatal("expected a beecon_csrf cookie to be set")
		}
		if !session.HttpOnly {
			t.Error("beecon_session cookie must be HttpOnly")
		}
		if session.SameSite != http.SameSiteStrictMode {
			t.Errorf("beecon_session SameSite = %v, want Strict", session.SameSite)
		}
		if csrf.HttpOnly {
			t.Error("beecon_csrf cookie must NOT be HttpOnly — the SPA reads it for the CSRF double-submit header")
		}
	})

	t.Run("the session cookie authenticates GET /auth/me as the logged-in operator", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionCookies)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var me sessionOperatorDTO
		if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
			t.Fatalf("decode /auth/me body: %v; body=%s", err, w.Body.String())
		}
		if me.ID != bootstrapped.ID {
			t.Errorf("id = %q, want %q", me.ID, bootstrapped.ID)
		}
		if me.Email != "founder@example.com" {
			t.Errorf("email = %q, want %q", me.Email, "founder@example.com")
		}
	})

	t.Run("the session cookie alone (no admin key) authenticates a general console read", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", "", "", sessionCookies)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("GET /auth/me without any session cookie is unauthorized", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("bootstrapping with a password shorter than the minimum is rejected naming the requirement", func(t *testing.T) {
		// Exercised against a fresh app instance (this one already has its
		// one operator, so Bootstrap would 409 before ever validating the
		// password) — the point here is the validation-order/response shape
		// on a still-bootstrappable installation.
		freshWired := support.BootApp(t)
		w := doOperatorAuthRequest(t, freshWired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
			`{"email":"someone@example.com","password":"short"}`, nil)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error body: %v; body=%s", err, w.Body.String())
		}
		if !strings.Contains(env.Error.Message, "validation") {
			t.Errorf("error.message = %q, want it to indicate a validation failure", env.Error.Message)
		}
	})

	// --- Security-critical: only the SHA-256 hash of the session token is
	// ever persisted — a database dump never contains the raw opaque token a
	// client's cookie carries (PD51). ---

	t.Run("a real database dump of operator_sessions never contains the raw opaque session token, only its SHA-256 hash", func(t *testing.T) {
		var rawToken string
		for _, c := range sessionCookies {
			if c.Name == "beecon_session" {
				rawToken = c.Value
			}
		}
		if rawToken == "" {
			t.Fatal("test fixture bug: no beecon_session cookie value captured from the earlier login subtest")
		}

		rows, err := wired.DB.QueryContext(context.Background(),
			"SELECT id, operator_id, token_hash, csrf_token FROM operator_sessions")
		if err != nil {
			t.Fatalf("dump operator_sessions: %v", err)
		}
		defer rows.Close()

		rowCount := 0
		for rows.Next() {
			rowCount++
			var id, operatorID, tokenHash, csrfToken string
			if err := rows.Scan(&id, &operatorID, &tokenHash, &csrfToken); err != nil {
				t.Fatalf("scan dumped row: %v", err)
			}
			for column, value := range map[string]string{
				"id":          id,
				"operator_id": operatorID,
				"token_hash":  tokenHash,
				"csrf_token":  csrfToken,
			} {
				if strings.Contains(value, rawToken) {
					t.Errorf("column %q of the database dump contains the raw session token %q — it must never be persisted", column, rawToken)
				}
			}
			wantHash := sha256.Sum256([]byte(rawToken))
			if tokenHash != hex.EncodeToString(wantHash[:]) {
				t.Errorf("token_hash = %q, want the hex-encoded SHA-256 of the raw session token (PD51) — storage scheme drifted", tokenHash)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate dumped rows: %v", err)
		}
		if rowCount != 1 {
			t.Fatalf("dumped %d operator_sessions rows, want exactly 1", rowCount)
		}
	})
}
