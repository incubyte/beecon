// Package connections owns the Connection entity: a user's attempt to
// authorize one Integration, from initiate through its full lifecycle
// (Slice 4) — ACTIVE, DISCONNECTED (disabled), EXPIRED (a failed refresh),
// and back to ACTIVE again via Reconnect, always under the same immutable
// id (PD19).
package connections

import (
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// ConnectionID is minted only by Initiate, and never changes across re-auth
// (a stable id for the life of the connection).
type ConnectionID string

// Status is a Connection's lifecycle state (PD11, PD19).
type Status string

const (
	StatusInitiated    Status = "INITIATED"
	StatusActive       Status = "ACTIVE"
	StatusExpired      Status = "EXPIRED"
	StatusDisconnected Status = "DISCONNECTED"
)

// Connection is the domain aggregate root: one user's attempt to authorize
// one Integration. ConnectToken is the single-use token embedded in the
// connect-page redirectUrl — minted at Initiate, and re-minted (against the
// same connection id) by Reconnect (PD19); ConnectTokenExpiresAt and
// ConnectTokenUsed track that current token's own TTL and single-use
// consumption independently of Status, so a Reconnect attempt can run its
// full handshake without ever reporting a previously ACTIVE connection as
// anything other than ACTIVE until the handshake actually completes (AC6).
// EncryptedAccessToken and EncryptedRefreshToken hold only Vault ciphertext
// (AC10: the raw token values are never held on this struct); TokenExpiresAt
// is the access token's own expiry (PD18) — nil only for a connection
// migrated before this field existed, treated as already expired so the
// first execution against it self-heals. AccountEmail and AccountDisplayName
// are populated once the OAuth callback activates the connection (PD9, AC6).
// EncryptedParams holds the connect page's collected pre-auth param values
// (Slice 3, PD13/PD17), vault-encrypted as one JSON blob under the same rule
// — never plaintext, never held anywhere else; empty when the provider
// declares no expectedParams, before the connect page's form has been
// submitted, or after Reconnect has reset it for re-collection.
// ReconciledAt is the last time ReconcileOnce re-verified this connection
// against its provider (PD37); nil means never reconciled (due immediately).
type Connection struct {
	ID                    ConnectionID
	OrgID                 organizations.OrgID
	UserID                organizations.UserID
	IntegrationID         catalog.IntegrationID
	ProviderSlug          string
	Status                Status
	RedirectURI           string
	ConnectToken          string
	ConnectTokenExpiresAt time.Time
	ConnectTokenUsed      bool
	EncryptedAccessToken  string
	EncryptedRefreshToken string
	TokenExpiresAt        *time.Time
	AccountEmail          string
	AccountDisplayName    string
	EncryptedParams       string
	ReconciledAt          *time.Time
	CreatedAt             time.Time
}

// NewConnection constructs a freshly initiated Connection. Callers are
// responsible for validating the user, integration, and redirectURI before
// calling this — it always starts INITIATED (PD11).
func NewConnection(
	id ConnectionID,
	org organizations.OrgID,
	userID organizations.UserID,
	integrationID catalog.IntegrationID,
	providerSlug string,
	redirectURI string,
	connectToken string,
	now time.Time,
) Connection {
	return Connection{
		ID:                    id,
		OrgID:                 org,
		UserID:                userID,
		IntegrationID:         integrationID,
		ProviderSlug:          providerSlug,
		Status:                StatusInitiated,
		RedirectURI:           redirectURI,
		ConnectToken:          connectToken,
		ConnectTokenExpiresAt: now.Add(ConnectLinkTTL),
		CreatedAt:             now,
	}
}

// Activate returns a copy of c transitioned to ACTIVE, carrying the vault's
// encrypted tokens, the access token's own expiry (PD18), and the account
// metadata the OAuth callback captured (AC4, AC5, AC6). It also marks the
// connect token that carried this handshake as used, so it can never
// authorize a second one (AC2) — whether this was the connection's original
// Initiate or a later Reconnect (PD19). c's id, org, user, and integration
// are untouched — activation never mints a second id.
func (c Connection) Activate(encryptedAccessToken, encryptedRefreshToken, accountEmail, accountDisplayName string, tokenExpiresAt time.Time) Connection {
	activated := c
	activated.Status = StatusActive
	activated.EncryptedAccessToken = encryptedAccessToken
	activated.EncryptedRefreshToken = encryptedRefreshToken
	activated.TokenExpiresAt = &tokenExpiresAt
	activated.AccountEmail = accountEmail
	activated.AccountDisplayName = accountDisplayName
	activated.ConnectTokenUsed = true
	return activated
}

// WithParams returns a copy of c carrying encryptedParams — the vault
// ciphertext of the pre-auth param values collected via the connect page's
// param-collection form (Slice 3, AC7), submitted before the OAuth handshake
// proceeds. c's id, status, and every other field are untouched.
func (c Connection) WithParams(encryptedParams string) Connection {
	updated := c
	updated.EncryptedParams = encryptedParams
	return updated
}

// Disable returns a copy of c transitioned to DISCONNECTED (Slice 4, PD19):
// its stored tokens and account metadata are retained — disable has no
// separate "enable", only Reconnect brings a connection back to ACTIVE, so
// there is nothing to restore later if not this. Only c's status changes;
// execution against the result surfaces PD19's status-explaining failure
// rather than calling the provider.
func (c Connection) Disable() Connection {
	disabled := c
	disabled.Status = StatusDisconnected
	return disabled
}

// MarkExpired returns a copy of c transitioned to EXPIRED: a refresh attempt
// (refresh.go) calls this when the provider rejects the refresh grant (e.g.
// a revoked refresh token, AC9). Its now-unusable tokens are left in place —
// the point of this transition is to stop the connection reporting ACTIVE,
// not to erase anything; Reconnect (PD19) is what replaces them.
func (c Connection) MarkExpired() Connection {
	expired := c
	expired.Status = StatusExpired
	return expired
}

// RefreshTokens returns a copy of c carrying a freshly refreshed access
// token — and, only when the provider rotated it, a new refresh token
// (PD18, AC8) — with the access token's expiry updated. c's status is left
// untouched: RefreshTokens is only ever called on a Connection already
// ACTIVE.
func (c Connection) RefreshTokens(encryptedAccessToken, encryptedRefreshToken string, tokenExpiresAt time.Time) Connection {
	refreshed := c
	refreshed.EncryptedAccessToken = encryptedAccessToken
	refreshed.EncryptedRefreshToken = encryptedRefreshToken
	refreshed.TokenExpiresAt = &tokenExpiresAt
	return refreshed
}

// MarkReconciled returns a copy of c with ReconciledAt advanced to now
// (PD37).
func (c Connection) MarkReconciled(now time.Time) Connection {
	reconciled := c
	reconciled.ReconciledAt = &now
	return reconciled
}

// CanReconnect reports whether Reconnect may start a fresh handshake
// against c (PD19): only from ACTIVE, EXPIRED, or DISCONNECTED. An INITIATED
// connection's own initiate attempt is still open — it has no finished
// attempt to redo.
func (c Connection) CanReconnect() bool {
	return c.Status == StatusActive || c.Status == StatusExpired || c.Status == StatusDisconnected
}

// PrepareReconnect returns a copy of c ready for a fresh connect-page
// handshake under its own immutable id (PD19, AC4): a new single-use connect
// token with its own expiry, redirectURI updated to this reconnect attempt's
// own, and EncryptedParams cleared so a provider that declares expected
// params collects them again (values re-collected, per the spec's reconnect
// note). c's status and existing tokens/account metadata are left exactly as
// they are — only a successful HandleCallback (Activate) changes them, so an
// abandoned or failed reconnect leaves a previously ACTIVE connection
// working throughout (AC6).
func (c Connection) PrepareReconnect(connectToken, redirectURI string, connectTokenExpiresAt time.Time) Connection {
	prepared := c
	prepared.ConnectToken = connectToken
	prepared.RedirectURI = redirectURI
	prepared.ConnectTokenExpiresAt = connectTokenExpiresAt
	prepared.ConnectTokenUsed = false
	prepared.EncryptedParams = ""
	return prepared
}
