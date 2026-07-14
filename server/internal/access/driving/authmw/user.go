package authmw

import (
	"context"
	"net/http"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// VerifyUserToken authenticates a presented user-token JWT (PD20) and
// returns the organization and user it identifies. Satisfied by
// (*access.Facade).VerifyUserToken, passed as a plain function value so
// authmw carries no import on the access module's concrete Facade type —
// same seam as Verify for org API keys.
type VerifyUserToken func(ctx context.Context, token string) (organizations.OrgID, organizations.UserID, error)

// UserAuth guards user-scoped endpoints: the request must carry
// `Authorization: Bearer <user-token JWT>`. A valid, unexpired token's
// organization and user are injected into the request context via
// organizations.WithOrgID and organizations.WithUserID; handlers must read
// them only through organizations.OrgIDFromContext and
// organizations.UserIDFromContext — never from path, body, or query. A
// missing, malformed, tampered, wrong-secret, or expired token is rejected
// with the PD5 unauthorized envelope; an infrastructure failure while
// verifying (e.g. a database error) surfaces as 500, never 401 (PD38b,
// Phase 2 review carry-forward) — see writeAuthError.
func UserAuth(verify VerifyUserToken) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or malformed user token"))
				return
			}
			org, userID, err := verify(r.Context(), token)
			if err != nil {
				writeAuthError(w, err)
				return
			}
			ctx := organizations.WithUserID(organizations.WithOrgID(r.Context(), org), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OrgOrUser guards routes reachable by either an org API key or a
// user-scoped browser token (PD20): tools list, expected-params, initiate
// connection, list/get own connections, and reconnect own connection all
// accept both. It tries verifyOrg first — a user-token JWT is cheaply
// rejected there, since it never carries access.SecretPrefix — then falls
// back to verifyUser; a request that satisfies neither is rejected with the
// PD5 unauthorized envelope. Handlers distinguish the two paths via
// organizations.UserIDFromContext: present only when a user token
// authenticated the request. An infrastructure failure while verifying
// either credential form (e.g. a database error) surfaces as 500, never
// 401 (PD38b, Phase 2 review carry-forward): OrgOrUser tries both paths
// before giving up, so it only reports 401 once both have failed as
// genuine business rejections — if either failed for an infrastructure
// reason instead, that is reported as 500 rather than being silently
// swallowed by falling through to the other path's own rejection.
func OrgOrUser(verifyOrg Verify, verifyUser VerifyUserToken) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or malformed authorization header"))
				return
			}
			org, orgErr := verifyOrg(r.Context(), token)
			if orgErr == nil {
				next.ServeHTTP(w, r.WithContext(organizations.WithOrgID(r.Context(), org)))
				return
			}
			userOrg, userID, userErr := verifyUser(r.Context(), token)
			if userErr == nil {
				ctx := organizations.WithUserID(organizations.WithOrgID(r.Context(), userOrg), userID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if isInfrastructureFailure(orgErr) || isInfrastructureFailure(userErr) {
				httpx.WriteDomainError(w, nil)
				return
			}
			httpx.WriteDomainError(w, httpx.Unauthorized("invalid or expired credential"))
		})
	}
}
