package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"beecon/internal/httpx"
)

const bearerPrefix = "Bearer "

// RequireBearerToken guards a registry-service route with a single static
// bearer token (PD63's publish token, PD67's installation-facing API key),
// compared in constant time. The registry service depends on no domain
// module (PD59), so it cannot reuse access/driving/authmw — this is its own
// small, self-contained auth surface, mirroring authmw.AdminAuth's exact
// shape and comparison discipline. A missing or wrong token is rejected
// with the PD5 unauthorized envelope before the handler ever runs, so an
// unauthorized pull returns no bundle data at all.
func RequireBearerToken(expected string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isValidBearerToken(r.Header.Get("Authorization"), expected) {
				httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid registry credential"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isValidBearerToken(authorizationHeader, expected string) bool {
	presented, ok := strings.CutPrefix(authorizationHeader, bearerPrefix)
	if !ok || presented == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}
