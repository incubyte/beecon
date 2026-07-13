package connections

import (
	"errors"
	"fmt"
	"net/http"

	"beecon/internal/httpx"
)

// Machine-readable error codes (PD5 convention).
const (
	CodeNotFound         = "not_found"
	CodeValidationFailed = "validation_failed"
)

// ErrNotFound is returned when no connection matches the requested id within
// the caller's organization. A connection belonging to another organization
// surfaces identically — no existence leak (PD5).
func ErrNotFound() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeNotFound, "connection not found")
}

// ErrRedirectURINotAllowed is returned when Initiate is asked to bind a
// connection attempt to a redirectUri not on the organization's allow-list
// (PD4). An empty allow-list always produces this error.
func ErrRedirectURINotAllowed() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "redirectUri", "issue": "not in organization's allowed redirect uris"})
}

// ErrValidation is the shared PD5 validation_failed shape for the
// connections module's request-level checks.
func ErrValidation(field, issue string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": field, "issue": issue})
}

// Machine-readable error codes for the OAuth handshake (Slice 4). These
// power connectweb's error page (via DomainError.Message), not a JSON API
// response — the codes stay consistent with the rest of the module even
// though no consumer switches on them today.
const (
	CodeConnectLinkInvalid          = "connect_link_invalid"
	CodeConnectLinkExpired          = "connect_link_expired"
	CodeConnectLinkAlreadyCompleted = "connect_link_already_completed"
	CodeOAuthStateMissing           = "oauth_state_missing"
	CodeOAuthStateUnknown           = "oauth_state_unknown"
	CodeOAuthStateExpired           = "oauth_state_expired"
	CodeOAuthStateAlreadyUsed       = "oauth_state_already_used"
	CodeOAuthTokenExchangeFailed    = "oauth_token_exchange_failed"
)

// ErrConnectLinkInvalid is returned when OpenConnectPage is given a connect
// token that names no connection (AC2).
func ErrConnectLinkInvalid() *httpx.DomainError {
	return httpx.New(http.StatusNotFound, CodeConnectLinkInvalid, "this connection link is invalid")
}

// ErrConnectLinkExpired is returned when OpenConnectPage is given a connect
// token past its ConnectLinkTTL (AC2).
func ErrConnectLinkExpired() *httpx.DomainError {
	return httpx.New(http.StatusGone, CodeConnectLinkExpired, "this connection link has expired — please start again")
}

// ErrConnectLinkAlreadyCompleted is returned when OpenConnectPage is given a
// connect token for a connection that is no longer INITIATED (AC2).
func ErrConnectLinkAlreadyCompleted() *httpx.DomainError {
	return httpx.New(http.StatusGone, CodeConnectLinkAlreadyCompleted, "this connection has already been completed")
}

// ErrStateMissing is returned when the OAuth callback carries no state
// parameter at all (AC7).
func ErrStateMissing() *httpx.DomainError {
	return httpx.New(http.StatusBadRequest, CodeOAuthStateMissing, "this connection request is missing its security token")
}

// ErrStateUnknown is returned when the OAuth callback's state parameter
// names no CSRF state Beecon minted (AC7).
func ErrStateUnknown() *httpx.DomainError {
	return httpx.New(http.StatusBadRequest, CodeOAuthStateUnknown, "this connection request's security token is not recognized")
}

// ErrStateExpired is returned when the OAuth callback's state parameter has
// passed its OAuthStateTTL (AC7).
func ErrStateExpired() *httpx.DomainError {
	return httpx.New(http.StatusGone, CodeOAuthStateExpired, "this connection request has expired — please start again")
}

// ErrStateAlreadyUsed is returned when the OAuth callback's state parameter
// has already been consumed by a previous callback (AC7).
func ErrStateAlreadyUsed() *httpx.DomainError {
	return httpx.New(http.StatusGone, CodeOAuthStateAlreadyUsed, "this connection request has already been used")
}

// ErrTokenExchangeFailed is returned when the provider rejects the
// authorization code, or the account-info fetch fails, during the OAuth
// callback (AC9). The connection stays INITIATED (PD11).
func ErrTokenExchangeFailed() *httpx.DomainError {
	return httpx.New(http.StatusBadGateway, CodeOAuthTokenExchangeFailed, "we couldn't complete the connection with the provider — please try again")
}

// CodeMissingRequiredParams names an expected pre-auth param SubmitParams
// requires but the connect page's form omitted (Slice 3, AC4).
const CodeMissingRequiredParams = "missing_required_params"

// ErrMissingRequiredParams is returned when SubmitParams is given values
// missing one or more of the provider definition's required expected params
// (AC4): connectweb re-renders the param form with each named field marked
// invalid, and never forwards to the provider. missing names every required
// field that was empty or absent, retrievable via MissingParamFields.
func ErrMissingRequiredParams(missing []string) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeMissingRequiredParams, "validation failed").
		WithDetails(map[string]any{"field": "params", "missing": missing})
}

// MissingParamFields extracts the list of expected-param names
// ErrMissingRequiredParams carries, so connectweb can mark each one invalid
// without switching on error internals itself. ok is false for any other
// error.
func MissingParamFields(err error) ([]string, bool) {
	var domainErr *httpx.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != CodeMissingRequiredParams {
		return nil, false
	}
	missing, _ := domainErr.Details["missing"].([]string)
	return missing, true
}

// ErrInvalidCursor is returned when List is given a pagination cursor that
// is not valid base64, or does not decode to the created_at/id shape List
// itself encodes (Slice 4, AC1).
func ErrInvalidCursor() *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeValidationFailed, "validation failed").
		WithDetails(map[string]any{"field": "cursor", "issue": "malformed pagination cursor"})
}

// CodeReconnectNotAllowed is Reconnect's error code when a Connection's
// current status doesn't allow a fresh handshake (PD19).
const CodeReconnectNotAllowed = "reconnect_not_allowed"

// ErrReconnectNotAllowed is returned when Reconnect is asked to start a
// fresh handshake against a Connection that is still INITIATED — its own
// initiate attempt has never finished, so there is nothing to redo (PD19:
// reconnect is only defined for ACTIVE, EXPIRED, or DISCONNECTED).
func ErrReconnectNotAllowed(status Status) *httpx.DomainError {
	return httpx.New(http.StatusUnprocessableEntity, CodeReconnectNotAllowed, "this connection cannot be reconnected right now").
		WithDetails(map[string]any{"field": "status", "issue": fmt.Sprintf("connection is %s", status)})
}
