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
// with the PD5 unauthorized envelope.
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
				httpx.WriteDomainError(w, httpx.Unauthorized("invalid or expired user token"))
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
// authenticated the request.
func OrgOrUser(verifyOrg Verify, verifyUser VerifyUserToken) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or malformed authorization header"))
				return
			}
			if org, err := verifyOrg(r.Context(), token); err == nil {
				next.ServeHTTP(w, r.WithContext(organizations.WithOrgID(r.Context(), org)))
				return
			}
			if org, userID, err := verifyUser(r.Context(), token); err == nil {
				ctx := organizations.WithUserID(organizations.WithOrgID(r.Context(), org), userID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			httpx.WriteDomainError(w, httpx.Unauthorized("invalid or expired credential"))
		})
	}
}
