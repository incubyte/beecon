package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	"beecon/internal/access/driving/authmw"
	"beecon/internal/httpx"
)

// OperatorHandler serves operator bootstrap/login/session-probe (Slice 1),
// logout (Slice 2), and operator account management/break-glass reset
// (Slice 4: ListOperators/CreateOperator/ChangeMyPassword/Deactivate/
// ResetPassword). It depends only on the OperatorFacade, the shared
// error renderer, and
// whether cookies should carry Secure (derived from BEECON_BASE_URL's
// scheme, FD-E) — the session/CSRF cookie *shape* itself lives in
// authmw/session_cookie.go, the same file authmw.ConsoleAuth reads those
// cookies through, so the two ends of the contract can never drift apart.
type OperatorHandler struct {
	facade        *access.OperatorFacade
	errors        *httpx.ErrorRenderer
	secureCookies bool
}

func NewOperatorHandler(facade *access.OperatorFacade, errors *httpx.ErrorRenderer, secureCookies bool) *OperatorHandler {
	return &OperatorHandler{facade: facade, errors: errors, secureCookies: secureCookies}
}

// Bootstrap handles POST /api/v1/operators/bootstrap (AdminAuth-guarded,
// PD54): creates the installation's first operator account. Rejected with
// 409 once one already exists (bootstrap is first-account-only).
func (h *OperatorHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequestDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, access.ErrValidation("email", "request body must be valid JSON"))
		return
	}
	bootstrapped, err := h.facade.Bootstrap(r.Context(), req.Email, req.Password)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toBootstrappedOperatorDTO(bootstrapped))
}

// Login handles POST /api/v1/auth/login (unauthenticated): on success it
// sets the PD52 session + CSRF cookies and answers 204 — the opaque session
// token itself never appears in the response body, only in the Set-Cookie
// header.
func (h *OperatorHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequestDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, access.ErrInvalidCredentials())
		return
	}
	session, err := h.facade.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	authmw.SetSessionCookies(w, session.Token, session.CSRFToken, session.ExpiresAt, h.secureCookies)
	w.WriteHeader(http.StatusNoContent)
}

// Logout handles POST /api/v1/auth/logout (authmw.OperatorSession-guarded in
// production, so a request normally arrives here only with a still-valid
// session): it reads the session token straight from the cookie itself
// (never from context) and revokes exactly that session, then always clears
// both PD52 cookies and answers 204 — idempotent (Slice 2, AC7): a missing,
// already-revoked, or unknown token is not an error, so a repeated logout (or
// one called with no session cookie at all, as this handler is unit-tested
// directly against — see operator_handler_test.go's convention) still
// answers 204, never a 500.
func (h *OperatorHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if token, ok := authmw.SessionTokenFromRequest(r); ok {
		if err := h.facade.Logout(r.Context(), token); err != nil {
			h.errors.WriteError(w, r, err)
			return
		}
	}
	authmw.ClearSessionCookies(w, h.secureCookies)
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /api/v1/auth/me (authmw.OperatorSession-guarded): the
// SPA's session probe — 200 with {id, email} when the session cookie is
// valid, otherwise the middleware itself already answered 401.
func (h *OperatorHandler) Me(w http.ResponseWriter, r *http.Request) {
	operatorID, ok := access.OperatorFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, access.ErrSessionUnauthorized())
		return
	}
	profile, err := h.facade.Me(r.Context(), operatorID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOperatorProfileDTO(profile))
}

// ListOperators handles GET /api/v1/operators (authmw.OperatorSession-guarded,
// Slice 4 AC3): every operator account, email/status/created only — never a
// password hash.
func (h *OperatorHandler) ListOperators(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.facade.ListOperators(r.Context())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOperatorsListDTO(summaries))
}

// CreateOperator handles POST /api/v1/operators (authmw.OperatorSession-guarded,
// Slice 4 AC1): a logged-in operator creates another ACTIVE operator
// account. Rejected with 409 when the email is already in use (AC2).
func (h *OperatorHandler) CreateOperator(w http.ResponseWriter, r *http.Request) {
	var req createOperatorRequestDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, access.ErrValidation("email", "request body must be valid JSON"))
		return
	}
	created, err := h.facade.CreateOperator(r.Context(), req.Email, req.Password)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toCreatedOperatorDTO(created))
}

// ChangeMyPassword handles POST /api/v1/operators/me/password
// (authmw.OperatorSession-guarded, Slice 4 AC4): the acting operator changes
// their own password after presenting their current one. Reads the acting
// operator's id and session id straight from context (injected by
// ConsoleAuth/OperatorSession) — never from the request body — so the
// caller can only ever change their own password, and
// RevokeAllForOperatorExcept keeps exactly the session making this call
// alive (closes the carried-forward Slice 2 AC4).
func (h *OperatorHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	operatorID, ok := access.OperatorFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, access.ErrSessionUnauthorized())
		return
	}
	sessionID, _ := access.OperatorSessionFromContext(r.Context())
	var req changeMyPasswordRequestDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, access.ErrValidation("newPassword", "request body must be valid JSON"))
		return
	}
	if err := h.facade.ChangeMyPassword(r.Context(), operatorID, sessionID, req.CurrentPassword, req.NewPassword); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Deactivate handles POST /api/v1/operators/{opId}/deactivate
// (authmw.OperatorSession-guarded, Slice 4 AC5): disables another operator
// account. Rejected with 409 when the target is the installation's last
// remaining ACTIVE operator (AC6).
func (h *OperatorHandler) Deactivate(w http.ResponseWriter, r *http.Request) {
	targetOperatorID := access.OperatorID(chi.URLParam(r, "opId"))
	if err := h.facade.Deactivate(r.Context(), targetOperatorID); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetPassword handles the break-glass POST
// /api/v1/operators/{opId}/reset-password (authmw.AdminAuth-guarded, FD-B):
// unlike bootstrap, this works even after operators exist — it is the
// installation admin key's one remaining console-adjacent write once
// operators exist, for genuine locked-out recovery.
func (h *OperatorHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	targetOperatorID := access.OperatorID(chi.URLParam(r, "opId"))
	var req resetPasswordRequestDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, access.ErrValidation("newPassword", "request body must be valid JSON"))
		return
	}
	if err := h.facade.ResetPassword(r.Context(), targetOperatorID, req.NewPassword); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
