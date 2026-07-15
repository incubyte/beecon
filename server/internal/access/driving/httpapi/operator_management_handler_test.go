// Package httpapi (in-package test, see operator_handler_test.go's own
// header for the newOperatorTestFacade/newOperatorTestHandler conventions
// reused here). This file covers Phase 5 Slice 4's OperatorHandler additions:
// ListOperators, CreateOperator, ChangeMyPassword (critically: the acting
// operator id AND session id are read from context, never the request body),
// Deactivate, and the break-glass ResetPassword.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	"beecon/internal/httpx"
)

// --- ListOperators (AC3). ---

func TestOperatorHandlerListOperators_Returns200WithoutAPasswordHashField(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	h := newOperatorHandlerFor(t, facade)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operators", nil)
	w := httptest.NewRecorder()
	h.ListOperators(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto operatorsListDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(dto.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(dto.Items))
	}
	if dto.Items[0].Email != "operator@example.com" {
		t.Errorf("email = %q, want %q", dto.Items[0].Email, "operator@example.com")
	}
	if dto.Items[0].Status != string(access.OperatorStatusActive) {
		t.Errorf("status = %q, want %q", dto.Items[0].Status, access.OperatorStatusActive)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") {
		t.Fatal("ListOperators' response body mentions \"password\" — a hash must never be exposed")
	}
}

// --- CreateOperator (AC1/AC2). ---

func TestOperatorHandlerCreateOperator_Returns201WithTheNewOperatorsIDAndEmailOnly(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	h := newOperatorHandlerFor(t, facade)

	w := doOperatorHandlerRequest(h.CreateOperator, `{"email":"Second@Example.com","password":"another correct horse battery"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto createdOperatorDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Email != "second@example.com" {
		t.Errorf("email = %q, want lowercased %q", dto.Email, "second@example.com")
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "password") {
		t.Fatal("CreateOperator's response body mentions \"password\" — it must carry only id and email")
	}
}

func TestOperatorHandlerCreateOperator_Returns409ForADuplicateEmail(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade) // "operator@example.com"
	h := newOperatorHandlerFor(t, facade)

	w := doOperatorHandlerRequest(h.CreateOperator, `{"email":"operator@example.com","password":"another correct horse battery"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeEmailExists {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeEmailExists)
	}
}

func TestOperatorHandlerCreateOperator_Returns422ForAShortPassword(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	h := newOperatorHandlerFor(t, facade)

	w := doOperatorHandlerRequest(h.CreateOperator, `{"email":"second@example.com","password":"short"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// --- ChangeMyPassword (Slice 4 AC4; critically: session id from CONTEXT,
// never the request body). ---

// newOperatorHandlerFor mirrors newOperatorTestHandler but takes an
// already-built facade (rather than building a fresh one), so a test can
// seed fixture state on the facade before wiring the handler under test.
func newOperatorHandlerFor(t *testing.T, facade *access.OperatorFacade) *OperatorHandler {
	t.Helper()
	return NewOperatorHandler(facade, testErrorRenderer(t), false)
}

func testErrorRenderer(t *testing.T) *httpx.ErrorRenderer {
	t.Helper()
	return httpx.NewErrorRenderer(nil)
}

func doChangeMyPasswordRequest(h *OperatorHandler, operatorID access.OperatorID, sessionID access.OperatorSessionID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operators/me/password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := access.WithOperator(req.Context(), operatorID)
	if sessionID != "" {
		ctx = access.WithOperatorSession(ctx, sessionID)
	}
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ChangeMyPassword(w, req)
	return w
}

func TestOperatorHandlerChangeMyPassword_Returns204AndKeepsTheActingSessionAliveWhileRevokingOthers(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	sessionS1, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login S1: unexpected error: %v", err)
	}
	sessionS2, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login S2: unexpected error: %v", err)
	}
	authenticatedS1, err := facade.VerifySession(context.Background(), sessionS1.Token)
	if err != nil {
		t.Fatalf("verify S1: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	w := doChangeMyPasswordRequest(h, bootstrapped, authenticatedS1.SessionID,
		`{"currentPassword":"correct horse battery staple","newPassword":"a brand new password entirely"}`)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if _, err := facade.VerifySession(context.Background(), sessionS1.Token); err != nil {
		t.Errorf("expected the ACTING session S1 to remain valid, got: %v", err)
	}
	if _, err := facade.VerifySession(context.Background(), sessionS2.Token); err == nil {
		t.Error("expected session S2 to be revoked, but it still verifies")
	}
}

// TestOperatorHandlerChangeMyPassword_IgnoresABodySuppliedSessionIDAndOnlyEverTrustsContext
// is the critical AC assertion: the acting session id is read exclusively
// from context (injected by ConsoleAuth/OperatorSession); changeMyPasswordRequestDTO
// carries no session-id field at all, so even a request body that tried to
// smuggle a different session id in has no way to affect which session
// survives — this test drives ChangeMyPassword with a body carrying an
// unrelated/bogus extra field to confirm the handler never looks for one.
func TestOperatorHandlerChangeMyPassword_IgnoresABodySuppliedSessionIDAndOnlyEverTrustsContext(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	sessionS1, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login S1: unexpected error: %v", err)
	}
	sessionS2, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login S2: unexpected error: %v", err)
	}
	authenticatedS1, err := facade.VerifySession(context.Background(), sessionS1.Token)
	if err != nil {
		t.Fatalf("verify S1: unexpected error: %v", err)
	}
	authenticatedS2, err := facade.VerifySession(context.Background(), sessionS2.Token)
	if err != nil {
		t.Fatalf("verify S2: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	// The request body names S2's own session id under a field the DTO
	// doesn't define — proving it is silently ignored: the request is made
	// with S1 injected into context, so S1 (not S2, despite the body) must be
	// the one that survives.
	bodyTryingToNameAnotherSession := `{"currentPassword":"correct horse battery staple","newPassword":"a brand new password entirely","currentSessionId":"` + string(authenticatedS2.SessionID) + `"}`
	w := doChangeMyPasswordRequest(h, bootstrapped, authenticatedS1.SessionID, bodyTryingToNameAnotherSession)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if _, err := facade.VerifySession(context.Background(), sessionS1.Token); err != nil {
		t.Errorf("expected S1 (the context-injected acting session) to survive regardless of the body, got: %v", err)
	}
	if _, err := facade.VerifySession(context.Background(), sessionS2.Token); err == nil {
		t.Error("expected S2 to be revoked — a body field naming it as the 'current session' must have no effect")
	}
}

func TestOperatorHandlerChangeMyPassword_Returns401ForAWrongCurrentPassword(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	session, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	authenticated, err := facade.VerifySession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("verify: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	w := doChangeMyPasswordRequest(h, bootstrapped, authenticated.SessionID,
		`{"currentPassword":"totally-wrong","newPassword":"a brand new password entirely"}`)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if _, err := facade.VerifySession(context.Background(), session.Token); err != nil {
		t.Errorf("expected the session to remain valid after a rejected password change, got: %v", err)
	}
}

func TestOperatorHandlerChangeMyPassword_Returns422ForATooShortNewPassword(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	session, err := facade.Login(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	authenticated, err := facade.VerifySession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("verify: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	w := doChangeMyPasswordRequest(h, bootstrapped, authenticated.SessionID,
		`{"currentPassword":"correct horse battery staple","newPassword":"short"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestOperatorHandlerChangeMyPassword_Returns401WhenNoOperatorIsInContext(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operators/me/password", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	h.ChangeMyPassword(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// --- Deactivate (AC5/AC6). ---

func doDeactivateRequest(h *OperatorHandler, targetOperatorID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operators/"+targetOperatorID+"/deactivate", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("opId", targetOperatorID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.Deactivate(w, req)
	return w
}

func TestOperatorHandlerDeactivate_Returns204AndTheTargetCanNoLongerLogIn(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	created, err := facade.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	w := doDeactivateRequest(h, string(created.ID))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if _, err := facade.Login(context.Background(), "second@example.com", "another correct horse battery"); err == nil {
		t.Error("expected the deactivated operator to no longer be able to log in")
	}
}

func TestOperatorHandlerDeactivate_Returns409ForTheLastActiveOperator(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	h := newOperatorHandlerFor(t, facade)

	w := doDeactivateRequest(h, string(bootstrapped))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeLastActiveOperator {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeLastActiveOperator)
	}
}

// --- ResetPassword (FD-B, break-glass). ---

func doResetPasswordRequest(h *OperatorHandler, targetOperatorID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operators/"+targetOperatorID+"/reset-password", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("opId", targetOperatorID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.ResetPassword(w, req)
	return w
}

func TestOperatorHandlerResetPassword_ReactivatesADisabledOperatorAndTheNewPasswordLogsIn(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapOneOperatorViaFacade(t, facade)
	created, err := facade.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}
	if err := facade.Deactivate(context.Background(), created.ID); err != nil {
		t.Fatalf("deactivate fixture: unexpected error: %v", err)
	}
	h := newOperatorHandlerFor(t, facade)

	w := doResetPasswordRequest(h, string(created.ID), `{"newPassword":"a freshly reset password"}`)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if _, err := facade.Login(context.Background(), "second@example.com", "a freshly reset password"); err != nil {
		t.Errorf("expected the reactivated operator to log in with the new password, got: %v", err)
	}
}

func TestOperatorHandlerResetPassword_Returns422ForATooShortNewPassword(t *testing.T) {
	facade := newOperatorTestFacade(t)
	bootstrapped := bootstrapOneOperatorViaFacadeReturningID(t, facade)
	h := newOperatorHandlerFor(t, facade)

	w := doResetPasswordRequest(h, string(bootstrapped), `{"newPassword":"short"}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

// bootstrapOneOperatorViaFacadeReturningID mirrors bootstrapOneOperatorViaFacade
// (operator_handler_test.go) but hands back the new operator's id, which
// several Slice 4 tests above need to call ChangeMyPassword/Deactivate/
// ResetPassword directly.
func bootstrapOneOperatorViaFacadeReturningID(t *testing.T, facade *access.OperatorFacade) access.OperatorID {
	t.Helper()
	bootstrapped, err := facade.Bootstrap(context.Background(), "operator@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("bootstrap fixture: unexpected error: %v", err)
	}
	return bootstrapped.ID
}
