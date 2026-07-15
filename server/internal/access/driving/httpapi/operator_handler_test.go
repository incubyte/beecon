// Package httpapi (in-package test, see handler_test.go's own header for
// why). This file covers Phase 5 Slice 1's OperatorHandler (Bootstrap/Login/
// Me): none of its three routes read a chi.URLParam, so these tests call the
// handler methods directly against an httptest.ResponseRecorder rather than
// mounting a chi router, the same way a plain net/http handler would be unit
// tested.
package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/access/driving/authmw"
	"beecon/internal/httpx"
)

const operatorHandlerFixedTimeRFC3339 = "2026-07-15T09:00:00Z"

// newOperatorTestFacade wires a real access.OperatorFacade over the in-memory
// ports with a fixed clock — mirroring operator_facade_test.go's own
// newOperatorFacade helper (a separate package, so duplicated here rather
// than shared) — so the handler-level tests exercise the real facade logic,
// not a stub.
func newOperatorTestFacade(t *testing.T) *access.OperatorFacade {
	t.Helper()
	fixed, err := time.Parse(time.RFC3339, operatorHandlerFixedTimeRFC3339)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	var n int
	newID := func(prefix string) func() string {
		return func() string {
			n++
			return prefix + string(rune('0'+n))
		}
	}
	return access.NewOperatorFacade(
		memory.NewOperatorRepository(),
		memory.NewOperatorSessionRepository(),
		newID("op_"),
		newID("opsess_"),
		func() time.Time { return fixed },
		time.Hour,
	)
}

func newOperatorTestHandler(t *testing.T, secureCookies bool) *OperatorHandler {
	t.Helper()
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return NewOperatorHandler(newOperatorTestFacade(t), errorRenderer, secureCookies)
}

func doOperatorHandlerRequest(handlerFunc http.HandlerFunc, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handlerFunc(w, req)
	return w
}

// --- Bootstrap. ---

func TestOperatorHandlerBootstrap_Returns201WithTheNewOperatorsIDAndEmailOnly(t *testing.T) {
	h := newOperatorTestHandler(t, false)

	w := doOperatorHandlerRequest(h.Bootstrap, `{"email":"Operator@Example.com","password":"correct horse battery staple"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto bootstrappedOperatorDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Email != "operator@example.com" {
		t.Errorf("email = %q, want lowercased %q", dto.Email, "operator@example.com")
	}
	if dto.ID == "" {
		t.Error("expected a non-empty operator id")
	}
	if strings.Contains(w.Body.String(), "correct horse battery staple") {
		t.Fatal("Bootstrap's response body contains the plaintext password — it must never appear in any API response")
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") {
		t.Fatal("Bootstrap's response body mentions \"password\" at all — the DTO must carry only id and email")
	}
}

func TestOperatorHandlerBootstrap_Returns409OnceAnOperatorAlreadyExists(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	first := doOperatorHandlerRequest(h.Bootstrap, `{"email":"first@example.com","password":"correct horse battery staple"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first bootstrap: status = %d, want %d; body=%s", first.Code, http.StatusCreated, first.Body.String())
	}

	w := doOperatorHandlerRequest(h.Bootstrap, `{"email":"second@example.com","password":"correct horse battery staple"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeOperatorExists {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeOperatorExists)
	}
}

func TestOperatorHandlerBootstrap_Returns422NamingTheRequirementForAShortPassword(t *testing.T) {
	h := newOperatorTestHandler(t, false)

	w := doOperatorHandlerRequest(h.Bootstrap, `{"email":"operator@example.com","password":"short"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	var body struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if body.Error.Details["field"] != "password" {
		t.Errorf("details.field = %v, want %q", body.Error.Details["field"], "password")
	}
	issue, _ := body.Error.Details["issue"].(string)
	if !strings.Contains(issue, "12") {
		t.Errorf("details.issue = %q, want it to name the minimum length (12)", issue)
	}
}

func TestOperatorHandlerBootstrap_Returns422ForAMalformedJSONBody(t *testing.T) {
	h := newOperatorTestHandler(t, false)

	w := doOperatorHandlerRequest(h.Bootstrap, `{not valid json`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// --- Login. ---

func bootstrapViaHandler(t *testing.T, h *OperatorHandler) {
	t.Helper()
	w := doOperatorHandlerRequest(h.Bootstrap, `{"email":"operator@example.com","password":"correct horse battery staple"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("bootstrap fixture: status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestOperatorHandlerLogin_Returns204AndSetsBothPD52CookiesOnSuccess(t *testing.T) {
	h := newOperatorTestHandler(t, true)
	bootstrapViaHandler(t, h)

	w := doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"correct horse battery staple"}`)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want an empty 204 body — the opaque session token must never appear in the response body", w.Body.String())
	}
	cookies := w.Result().Cookies()
	session := findCookie(cookies, authmw.SessionCookieName)
	csrf := findCookie(cookies, authmw.CSRFCookieName)
	if session == nil {
		t.Fatal("expected a beecon_session cookie to be set")
	}
	if csrf == nil {
		t.Fatal("expected a beecon_csrf cookie to be set")
	}
	if !session.HttpOnly {
		t.Error("beecon_session cookie must be HttpOnly")
	}
	if !session.Secure {
		t.Error("beecon_session cookie must carry Secure when secureCookies=true")
	}
	if session.SameSite != http.SameSiteStrictMode {
		t.Errorf("beecon_session SameSite = %v, want Strict", session.SameSite)
	}
	if csrf.HttpOnly {
		t.Error("beecon_csrf cookie must NOT be HttpOnly — the SPA reads it for the double-submit header")
	}
	if !csrf.Secure {
		t.Error("beecon_csrf cookie must carry Secure when secureCookies=true")
	}
	if session.Value == "" {
		t.Error("expected a non-empty session token value")
	}
}

func TestOperatorHandlerLogin_CookiesAreNotSecureWhenSecureCookiesIsFalse(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	bootstrapViaHandler(t, h)

	w := doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"correct horse battery staple"}`)

	session := findCookie(w.Result().Cookies(), authmw.SessionCookieName)
	if session == nil {
		t.Fatal("expected a beecon_session cookie to be set")
	}
	if session.Secure {
		t.Error("expected Secure=false when the handler is wired with secureCookies=false (FD-E, non-TLS local/dev)")
	}
}

func TestOperatorHandlerLogin_RejectsAWrongPasswordWithAGenericUnauthorized(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	bootstrapViaHandler(t, h)

	w := doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"totally-wrong-password"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Message != "invalid credentials" {
		t.Errorf("error.message = %q, want the generic %q", env.Error.Message, "invalid credentials")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("expected no cookies to be set on a rejected login")
	}
	if strings.Contains(w.Body.String(), "totally-wrong-password") {
		t.Fatal("Login's response body contains the plaintext attempted password — it must never appear in any API response")
	}
}

func TestOperatorHandlerLogin_RejectsAnUnknownEmailWithTheIdenticalGenericUnauthorized(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	bootstrapViaHandler(t, h)

	wrongPassword := doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"totally-wrong-password"}`)
	unknownEmail := doOperatorHandlerRequest(h.Login, `{"email":"nobody@example.com","password":"correct horse battery staple"}`)

	if wrongPassword.Code != unknownEmail.Code {
		t.Fatalf("wrong-password status %d != unknown-email status %d", wrongPassword.Code, unknownEmail.Code)
	}
	wrongPasswordEnv := decodeError(t, wrongPassword)
	unknownEmailEnv := decodeError(t, unknownEmail)
	if wrongPasswordEnv.Error.Code != unknownEmailEnv.Error.Code || wrongPasswordEnv.Error.Message != unknownEmailEnv.Error.Message {
		t.Fatalf("responses differ: wrong-password=%+v, unknown-email=%+v — the caller must not be able to tell which case occurred", wrongPasswordEnv.Error, unknownEmailEnv.Error)
	}
}

func TestOperatorHandlerLogin_Returns401ForAMalformedJSONBodyWithoutLeakingParseDetails(t *testing.T) {
	h := newOperatorTestHandler(t, false)

	w := doOperatorHandlerRequest(h.Login, `{not valid json`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- Me. ---

func TestOperatorHandlerMe_Returns200WithTheAuthenticatedOperatorsIdentity(t *testing.T) {
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	facade := newOperatorTestFacade(t)
	bootstrapped, err := facade.Bootstrap(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("bootstrap fixture: unexpected error: %v", err)
	}
	h := NewOperatorHandler(facade, errorRenderer, false)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	ctx := access.WithOperator(req.Context(), bootstrapped.ID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	h.Me(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto operatorProfileDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ID != string(bootstrapped.ID) {
		t.Errorf("id = %q, want %q", dto.ID, bootstrapped.ID)
	}
	if dto.Email != "operator@example.com" {
		t.Errorf("email = %q, want %q", dto.Email, "operator@example.com")
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") {
		t.Fatal("Me's response body mentions \"password\" — it must carry only id and email")
	}
}

func TestOperatorHandlerMe_Returns401WhenNoOperatorIsInContext(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	w := httptest.NewRecorder()

	h.Me(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- Logout (Slice 2, AC1/AC7: revokes server-side, always clears both
// cookies, idempotent). ---

// doOperatorHandlerLogoutRequest builds a POST /api/v1/auth/logout request,
// optionally carrying a beecon_session cookie (sessionToken == "" means no
// cookie at all — the idempotent "no session" case).
func doOperatorHandlerLogoutRequest(h *OperatorHandler, sessionToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	if sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: authmw.SessionCookieName, Value: sessionToken})
	}
	w := httptest.NewRecorder()
	h.Logout(w, req)
	return w
}

func assertBothPD52CookiesAreCleared(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	cookies := w.Result().Cookies()
	session := findCookie(cookies, authmw.SessionCookieName)
	csrf := findCookie(cookies, authmw.CSRFCookieName)
	if session == nil {
		t.Fatal("expected the beecon_session cookie to be cleared in the response")
	}
	if csrf == nil {
		t.Fatal("expected the beecon_csrf cookie to be cleared in the response")
	}
	if session.Value != "" {
		t.Errorf("beecon_session cookie value = %q, want empty", session.Value)
	}
	if csrf.Value != "" {
		t.Errorf("beecon_csrf cookie value = %q, want empty", csrf.Value)
	}
	if session.MaxAge >= 0 {
		t.Errorf("beecon_session MaxAge = %d, want a negative Max-Age (Max-Age=0 on the wire, immediate expiry)", session.MaxAge)
	}
	if csrf.MaxAge >= 0 {
		t.Errorf("beecon_csrf MaxAge = %d, want a negative Max-Age (Max-Age=0 on the wire, immediate expiry)", csrf.MaxAge)
	}
}

func TestOperatorHandlerLogout_Returns204AndClearsBothCookiesForALoggedInSession(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	session, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewOperatorHandler(facade, errorRenderer, false)

	w := doOperatorHandlerLogoutRequest(h, session.Token)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want an empty 204 body", w.Body.String())
	}
	assertBothPD52CookiesAreCleared(t, w)
}

// TestOperatorHandlerLogout_RevokesTheSessionServerSideSoTheSameCookieFailsAfterward
// is the crucial security assertion at the handler level: it keeps a
// reference to the real facade the handler was built with and confirms
// VerifySession itself rejects the exact same token after Logout answered
// 204 — proving the session died server-side, not merely that the response
// cleared a cookie (a cookie clear alone would not stop a copied token).
func TestOperatorHandlerLogout_RevokesTheSessionServerSideSoTheSameCookieFailsAfterward(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	session, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	if _, err := facade.VerifySession(context.Background(), session.Token); err != nil {
		t.Fatalf("precondition: expected the freshly minted session to verify, got: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewOperatorHandler(facade, errorRenderer, false)

	w := doOperatorHandlerLogoutRequest(h, session.Token)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}

	_, err = facade.VerifySession(context.Background(), session.Token)
	if err == nil {
		t.Fatal("expected the exact same cookie token to be rejected by VerifySession after Logout — the session must be revoked server-side, not merely have its cookie cleared in this one response")
	}
}

func TestOperatorHandlerLogout_Returns204AndClearsCookiesWhenNoSessionCookieIsPresent(t *testing.T) {
	h := newOperatorTestHandlerForLogout(t)

	w := doOperatorHandlerLogoutRequest(h, "")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	assertBothPD52CookiesAreCleared(t, w)
}

func TestOperatorHandlerLogout_Returns204ForAnUnknownSessionCookieValue(t *testing.T) {
	h := newOperatorTestHandlerForLogout(t)

	w := doOperatorHandlerLogoutRequest(h, "a-cookie-value-that-matches-no-session")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestOperatorHandlerLogout_Returns204OnASecondLogoutCallWithTheSameNowRevokedCookie(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	session, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewOperatorHandler(facade, errorRenderer, false)
	first := doOperatorHandlerLogoutRequest(h, session.Token)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first logout: status = %d, want %d; body=%s", first.Code, http.StatusNoContent, first.Body.String())
	}

	second := doOperatorHandlerLogoutRequest(h, session.Token)

	if second.Code != http.StatusNoContent {
		t.Fatalf("second logout: status = %d, want %d (idempotent, never a 500); body=%s", second.Code, http.StatusNoContent, second.Body.String())
	}
}

func newOperatorTestHandlerForLogout(t *testing.T) *OperatorHandler {
	t.Helper()
	return newOperatorTestHandler(t, false)
}

func bootstrapOneOperatorViaFacade(t *testing.T, facade *access.OperatorFacade) {
	t.Helper()
	if _, err := facade.Bootstrap(context.Background(), "operator@example.com", "correct horse battery staple"); err != nil {
		t.Fatalf("bootstrap fixture: unexpected error: %v", err)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}
