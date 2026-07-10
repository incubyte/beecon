// Package authmw is the shared HTTP auth middleware for installation- and
// org-level operations. It is a cross-cutting concern, not a feature, so it
// lives outside any single module's driving adapter.
package authmw

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"beecon/internal/httpx"
)

const bearerPrefix = "Bearer "

// AdminAuth guards installation-level endpoints (PD1): the request must carry
// `Authorization: Bearer <BEECON_ADMIN_API_KEY>`, compared in constant time.
// A missing or wrong key is rejected with the PD5 unauthorized envelope.
func AdminAuth(adminKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isValidAdminKey(r.Header.Get("Authorization"), adminKey) {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid admin key"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isValidAdminKey(authorizationHeader, adminKey string) bool {
	presented, ok := strings.CutPrefix(authorizationHeader, bearerPrefix)
	if !ok || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(adminKey)) == 1
}
