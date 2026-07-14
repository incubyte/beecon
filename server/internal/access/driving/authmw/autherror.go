package authmw

import (
	"errors"
	"net/http"

	"beecon/internal/httpx"
)

// writeAuthError renders a verification failure from Verify/VerifyUserToken
// (PD38b, Phase 2 review carry-forward): a business rejection — a missing,
// malformed, unknown, revoked, or expired credential — always comes back as
// a *httpx.DomainError (access.ErrUnauthorized()), so that case renders
// exactly as before. Anything else (a plain error, e.g. a database failure
// during the lookup) is an infrastructure failure, not a verdict on the
// credential, and must never be reported to the caller as unauthorized — it
// renders as the PD5 500 internal_error envelope instead, the same shape
// httpx.WriteDomainError(w, nil) already produces.
func writeAuthError(w http.ResponseWriter, err error) {
	var domainErr *httpx.DomainError
	if errors.As(err, &domainErr) {
		httpx.WriteDomainError(w, domainErr)
		return
	}
	httpx.WriteDomainError(w, nil)
}

// isInfrastructureFailure reports whether err represents a verification
// failure that is not a business rejection (PD38b): every business
// rejection Verify/VerifyUserToken return is a *httpx.DomainError, so
// anything else signals an infrastructure problem the caller must see as
// 500, not 401.
func isInfrastructureFailure(err error) bool {
	if err == nil {
		return false
	}
	var domainErr *httpx.DomainError
	return !errors.As(err, &domainErr)
}
