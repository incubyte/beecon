package authmw

import (
	"context"
	"crypto/subtle"
	"net/http"

	"beecon/internal/access"
	"beecon/internal/httpx"
)

// VerifySession authenticates a presented opaque session token and returns
// the operator it identifies (PD51/PD52). Satisfied by
// (*access.OperatorFacade).VerifySession, passed as a plain function value —
// the same seam Verify/VerifyUserToken already establish, so authmw carries
// no import on the access module's concrete OperatorFacade type.
type VerifySession func(ctx context.Context, token string) (access.AuthenticatedOperator, error)

// ConsoleAuth guards the Admin UI's general console surface (FD-A, §3 of the
// architecture doc): session-first — a valid beecon_session cookie
// authenticates and injects the operator into context via
// access.WithOperator; failing that, the installation admin key is accepted
// as a Bearer token ONLY while no operator account exists yet (the
// pre-bootstrap break-glass window, PD54) — operatorsExist is checked on
// every such request (not cached), so the admin key's demotion takes effect
// the moment Bootstrap succeeds, no restart needed (Slice 4, AC8). A request
// carrying neither a valid session nor a valid pre-bootstrap admin key is
// rejected with the PD5 unauthorized envelope.
//
// CSRF verification (Slice 3, PD52, §3 step 1) applies only to the
// session-authenticated branch, and only on mutating methods
// (POST/PUT/PATCH/DELETE) — GET/HEAD/OPTIONS never require a token. The
// admin-key Bearer branch below is deliberately NOT CSRF-checked: a Bearer
// token isn't cookie-borne, so a forged cross-site request can never carry
// it — only the ambient session cookie is the CSRF risk.
func ConsoleAuth(verify VerifySession, adminKey string, operatorsExist func(context.Context) (bool, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := SessionTokenFromRequest(r); ok {
				operator, err := verify(r.Context(), token)
				if err != nil {
					writeAuthError(w, err)
					return
				}
				if isMutatingMethod(r.Method) && !csrfTokenMatches(r.Header.Get("X-CSRF-Token"), operator.CSRFToken) {
					httpx.WriteDomainError(w, access.ErrCSRF())
					return
				}
				ctx := access.WithOperator(r.Context(), operator.OperatorID)
				ctx = access.WithOperatorSession(ctx, operator.SessionID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if isValidAdminKey(r.Header.Get("Authorization"), adminKey) {
				exists, err := operatorsExist(r.Context())
				if err != nil {
					httpx.WriteDomainError(w, nil)
					return
				}
				if exists {
					httpx.WriteDomainError(w, httpx.Unauthorized("the installation admin key no longer authenticates the console once an operator account exists"))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid session"))
		})
	}
}

// AdminOrConsoleAuth guards the server-to-server provisioning endpoints
// (create org, issue org API key, set redirect-URI allow-list): it accepts
// EITHER a valid operator session (so the Admin UI keeps working, with the
// same CSRF enforcement and operator injection as ConsoleAuth) OR the
// installation admin key as a Bearer token — and, unlike ConsoleAuth, the
// admin key authenticates these routes DURABLY (no post-bootstrap demotion),
// so installation automation can register orgs without an operator session.
// It is deliberately scoped to only those provisioning routes; the rest of
// the console stays on ConsoleAuth (admin key demoted once an operator exists).
func AdminOrConsoleAuth(verify VerifySession, adminKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := SessionTokenFromRequest(r); ok {
				operator, err := verify(r.Context(), token)
				if err != nil {
					writeAuthError(w, err)
					return
				}
				if isMutatingMethod(r.Method) && !csrfTokenMatches(r.Header.Get("X-CSRF-Token"), operator.CSRFToken) {
					httpx.WriteDomainError(w, access.ErrCSRF())
					return
				}
				ctx := access.WithOperator(r.Context(), operator.OperatorID)
				ctx = access.WithOperatorSession(ctx, operator.SessionID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if isValidAdminKey(r.Header.Get("Authorization"), adminKey) {
				next.ServeHTTP(w, r)
				return
			}
			httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid session or admin key"))
		})
	}
}

// OperatorSession guards routes that only ever accept a session cookie —
// never the admin key (FD-A, §3): /auth/me, /auth/logout (Slice 2),
// /operators/* (Slice 4). Unlike ConsoleAuth, there is no break-glass
// fallback here at all. Same Slice 3 CSRF check as ConsoleAuth's session
// branch: mutating methods must carry a matching X-CSRF-Token — this is what
// makes /auth/logout (a POST) CSRF-protected (spec Slice 3 AC5).
func OperatorSession(verify VerifySession) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := SessionTokenFromRequest(r)
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing session"))
				return
			}
			operator, err := verify(r.Context(), token)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			if isMutatingMethod(r.Method) && !csrfTokenMatches(r.Header.Get("X-CSRF-Token"), operator.CSRFToken) {
				httpx.WriteDomainError(w, access.ErrCSRF())
				return
			}
			ctx := access.WithOperator(r.Context(), operator.OperatorID)
			ctx = access.WithOperatorSession(ctx, operator.SessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isMutatingMethod reports whether method is one of the four state-changing
// HTTP verbs the CSRF double-submit check applies to (PD52, Slice 3 AC1/AC2)
// — GET, HEAD, and OPTIONS are safe reads and never require a token.
func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// csrfTokenMatches reports, in constant time, whether the caller-presented
// header value equals expected (the session's own CSRFToken, PD52). Either
// side being empty is always a mismatch — a session that (in principle)
// never got a CSRF token minted for it must never be treated as "no check
// needed".
func csrfTokenMatches(presented, expected string) bool {
	if presented == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}
