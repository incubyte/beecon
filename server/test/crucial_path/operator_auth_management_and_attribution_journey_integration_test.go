//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses doJSONRequest/doOperatorAuthRequest/wireErrorEnvelope/
// csrfTokenFrom already declared in this package's other files). This file
// tells Phase 5 Slice 4's story end to end against the real composition
// root: bootstrap operator A -> A logs in (S1) -> A creates operator B -> B
// logs in -> A changes A's own password (A's S1 still works, A's other
// sessions gone, B unaffected) -> A deactivates B (B's session dies, B can't
// log in) -> deactivating the last remaining ACTIVE operator is rejected
// with 409 -> the break-glass admin-key reset-password on B reactivates it
// and its new password works -> throughout, GET /metrics (the Prometheus
// scrape target, outside /api/v1) keeps accepting the admin key exactly as
// it always has, since AC8's demotion only applies to the general console
// surface.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/test/support"
)

// doOperatorSessionMutationRequest is doOperatorAuthRequest for a
// session-authenticated MUTATING call: it automatically echoes the
// beecon_csrf cookie's value as the X-CSRF-Token header (Slice 3, PD52),
// mirroring doOperatorAuthLogoutRequest's own convention for /auth/logout —
// every Slice 4 write below (create/change-password/deactivate) rides a
// session cookie and must carry this header to pass ConsoleAuth/
// OperatorSession's CSRF check.
func doOperatorSessionMutationRequest(t *testing.T, handler http.Handler, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

type createdOperatorDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type operatorsListEnvelope struct {
	Items []struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Status    string `json:"status"`
		CreatedAt string `json:"createdAt"`
	} `json:"items"`
}

func loginOperator(t *testing.T, handler http.Handler, email, password string) []*http.Cookie {
	t.Helper()
	w := doOperatorAuthRequest(t, handler, http.MethodPost, "/api/v1/auth/login", "",
		`{"email":"`+email+`","password":"`+password+`"}`, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("login %s: status = %d, want %d; body=%s", email, w.Code, http.StatusNoContent, w.Body.String())
	}
	return w.Result().Cookies()
}

func TestOperatorAuthManagementAndAttributionJourney(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey

	// --- bootstrap A ---
	w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
		`{"email":"A@example.com","password":"correct horse battery staple A"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap A: status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	// --- A logs in (S1) ---
	var sessionA1 []*http.Cookie
	t.Run("A logs in and mints session S1", func(t *testing.T) {
		sessionA1 = loginOperator(t, wired.Router, "a@example.com", "correct horse battery staple A")
	})

	// --- A creates operator B ---
	var operatorB createdOperatorDTO
	t.Run("A creates operator B", func(t *testing.T) {
		w := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators",
			`{"email":"B@example.com","password":"correct horse battery staple B"}`, sessionA1)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &operatorB); err != nil {
			t.Fatalf("decode created operator: %v; body=%s", err, w.Body.String())
		}
		if operatorB.Email != "b@example.com" {
			t.Errorf("email = %q, want lowercased %q", operatorB.Email, "b@example.com")
		}
	})

	t.Run("creating an operator with an already-used email is rejected", func(t *testing.T) {
		w := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators",
			`{"email":"B@example.com","password":"another perfectly fine password"}`, sessionA1)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
		}
	})

	t.Run("GET /operators lists both operators, email/status/created only, never a password hash", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/operators", "", "", sessionA1)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var page operatorsListEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode operators list: %v; body=%s", err, w.Body.String())
		}
		if len(page.Items) != 2 {
			t.Fatalf("got %d operators, want 2", len(page.Items))
		}
		if strings.Contains(strings.ToLower(w.Body.String()), "password") {
			t.Fatal("operators list response mentions \"password\" — a hash must never be exposed")
		}
	})

	// --- B logs in ---
	var sessionB []*http.Cookie
	t.Run("B logs in", func(t *testing.T) {
		sessionB = loginOperator(t, wired.Router, "b@example.com", "correct horse battery staple B")
	})

	// --- A changes A's own password: S1 (acting) survives, other A
	// sessions die, B is unaffected. ---
	var sessionA1AfterChange []*http.Cookie
	var sessionA2Stale []*http.Cookie
	t.Run("A changes A's own password: the acting session (S1) stays alive, a second A session dies, B is unaffected", func(t *testing.T) {
		// Mint a second session for A (S2) before the change, so we can prove
		// it dies while S1 (used to make the very call below) survives.
		sessionA2Stale = loginOperator(t, wired.Router, "a@example.com", "correct horse battery staple A")

		w := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/me/password",
			`{"currentPassword":"correct horse battery staple A","newPassword":"a brand new password for A"}`, sessionA1)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		sessionA1AfterChange = sessionA1

		// S1 (the acting session) still authenticates.
		meS1 := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionA1AfterChange)
		if meS1.Code != http.StatusOK {
			t.Fatalf("A's acting session S1 after its own password change: status = %d, want %d — it must survive its own ChangeMyPassword call", meS1.Code, http.StatusOK)
		}

		// S2 (a DIFFERENT session belonging to A) was revoked.
		meS2 := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionA2Stale)
		if meS2.Code != http.StatusUnauthorized {
			t.Fatalf("A's other session S2: status = %d, want %d — every OTHER session must be revoked by ChangeMyPassword", meS2.Code, http.StatusUnauthorized)
		}

		// B's own session is entirely unaffected.
		meB := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionB)
		if meB.Code != http.StatusOK {
			t.Fatalf("B's session: status = %d, want %d — B must be unaffected by A's own password change", meB.Code, http.StatusOK)
		}
	})

	t.Run("A's new password now logs in; A's old password no longer does", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"a@example.com","password":"a brand new password for A"}`, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("new password login: status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		oldPasswordLogin := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"a@example.com","password":"correct horse battery staple A"}`, nil)
		if oldPasswordLogin.Code != http.StatusUnauthorized {
			t.Fatalf("old password login: status = %d, want %d", oldPasswordLogin.Code, http.StatusUnauthorized)
		}
	})

	// --- A deactivates B: B's session dies, B can't log in. ---
	t.Run("A deactivates B: B's session dies immediately and B can no longer log in", func(t *testing.T) {
		w := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/"+operatorB.ID+"/deactivate", "", sessionA1AfterChange)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}

		meB := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionB)
		if meB.Code != http.StatusUnauthorized {
			t.Fatalf("B's session after deactivation: status = %d, want %d", meB.Code, http.StatusUnauthorized)
		}

		loginB := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"b@example.com","password":"correct horse battery staple B"}`, nil)
		if loginB.Code != http.StatusUnauthorized {
			t.Fatalf("B login after deactivation: status = %d, want %d", loginB.Code, http.StatusUnauthorized)
		}
	})

	// --- Deactivating the last remaining ACTIVE operator (A) is rejected. ---
	t.Run("deactivating the last remaining active operator (A) is rejected with 409", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/operators", "", "", sessionA1AfterChange)
		if w.Code != http.StatusOK {
			t.Fatalf("list operators: status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var page operatorsListEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode operators list: %v", err)
		}
		var operatorAID string
		for _, item := range page.Items {
			if item.Email == "a@example.com" {
				operatorAID = item.ID
			}
		}
		if operatorAID == "" {
			t.Fatal("could not find operator A's id in the list")
		}

		deactivateSelf := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/"+operatorAID+"/deactivate", "", sessionA1AfterChange)
		if deactivateSelf.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d (last-active-operator guard); body=%s", deactivateSelf.Code, http.StatusConflict, deactivateSelf.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(deactivateSelf.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error body: %v", err)
		}
		if env.Error.Code != "last_active_operator" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "last_active_operator")
		}

		// A's own session was left entirely untouched by the rejected call.
		meA := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/auth/me", "", "", sessionA1AfterChange)
		if meA.Code != http.StatusOK {
			t.Fatalf("A's session after the rejected self-deactivation: status = %d, want %d", meA.Code, http.StatusOK)
		}
	})

	// --- The break-glass admin-key reset-password recovers B. ---
	t.Run("the break-glass admin-key reset-password reactivates B and B logs in with the new password", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/"+operatorB.ID+"/reset-password", adminAuth,
			`{"newPassword":"a freshly reset password for B"}`, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}

		loginB := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"b@example.com","password":"a freshly reset password for B"}`, nil)
		if loginB.Code != http.StatusNoContent {
			t.Fatalf("B login with the reset password: status = %d, want %d; body=%s", loginB.Code, http.StatusNoContent, loginB.Body.String())
		}
	})

	// --- A session-authenticated (non-admin-key) caller can never reach
	// reset-password. ---
	t.Run("a session-authenticated caller (not the admin key) cannot hit reset-password", func(t *testing.T) {
		w := doOperatorSessionMutationRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/"+operatorB.ID+"/reset-password",
			`{"newPassword":"an operator should never be able to set this"}`, sessionA1AfterChange)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — reset-password must stay admin-key-only", w.Code, http.StatusUnauthorized)
		}
	})

	// --- AC8, confirmed at the /metrics boundary: the admin key keeps
	// scraping the machine endpoint throughout, even though it was demoted
	// off the general console the instant the first operator was bootstrapped. ---
	t.Run("GET /metrics still accepts the admin key even after operators exist", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/metrics", adminAuth, "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d — /metrics is a machine scrape target outside /api/v1 and stays admin-key-guarded regardless of AC8", w.Code, http.StatusOK)
		}
	})

	t.Run("but the admin key still cannot open the general console once operators exist", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations", adminAuth, "", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})
}
