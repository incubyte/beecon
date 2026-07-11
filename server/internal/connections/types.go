// Package connections owns the Connection entity: a user's attempt to
// authorize one Integration, from initiate through (in later slices) an
// ACTIVE connected account. Phase 1's statuses are INITIATED and ACTIVE only
// (PD11); EXPIRED/DISCONNECTED arrive in later phases.
package connections

import (
	"time"

	"beecon/internal/catalog"
	"beecon/internal/organizations"
)

// ConnectionID is minted only by Initiate, and never changes across re-auth
// (a stable id for the life of the connection).
type ConnectionID string

// Status is a Connection's lifecycle state (PD11).
type Status string

const (
	StatusInitiated Status = "INITIATED"
	StatusActive    Status = "ACTIVE"
)

// Connection is the domain aggregate root: one user's attempt to authorize
// one Integration. ConnectToken is the single-use token embedded in the
// connect-page redirectUrl minted at Initiate — it is bound to exactly this
// connection attempt. EncryptedAccessToken and EncryptedRefreshToken hold
// only Vault ciphertext (AC10: the raw token values are never held on this
// struct); AccountEmail and AccountDisplayName are populated once the OAuth
// callback activates the connection (PD9, AC6).
type Connection struct {
	ID                    ConnectionID
	OrgID                 organizations.OrgID
	UserID                organizations.UserID
	IntegrationID         catalog.IntegrationID
	ProviderSlug          string
	Status                Status
	RedirectURI           string
	ConnectToken          string
	EncryptedAccessToken  string
	EncryptedRefreshToken string
	AccountEmail          string
	AccountDisplayName    string
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
		ID:            id,
		OrgID:         org,
		UserID:        userID,
		IntegrationID: integrationID,
		ProviderSlug:  providerSlug,
		Status:        StatusInitiated,
		RedirectURI:   redirectURI,
		ConnectToken:  connectToken,
		CreatedAt:     now,
	}
}

// Activate returns a copy of c transitioned to ACTIVE, carrying the vault's
// encrypted tokens and the account metadata the OAuth callback captured
// (AC4, AC5, AC6). c's id, org, user, integration, and connect token are
// untouched — activation never mints a second id.
func (c Connection) Activate(encryptedAccessToken, encryptedRefreshToken, accountEmail, accountDisplayName string) Connection {
	activated := c
	activated.Status = StatusActive
	activated.EncryptedAccessToken = encryptedAccessToken
	activated.EncryptedRefreshToken = encryptedRefreshToken
	activated.AccountEmail = accountEmail
	activated.AccountDisplayName = accountDisplayName
	return activated
}
