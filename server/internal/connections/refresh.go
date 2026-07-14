// refresh.go implements PD18's on-demand token refresh via the provider's
// refresh_token grant. FD3 splits a failed grant into two outcomes: a
// permanent provider refusal (RefreshDenied) expires the connection through
// expireConnection's exactly-once funnel; anything else is transient and
// leaves the connection untouched, returning the error so the caller retries
// later — correcting Phase 2's original expire-on-any-error behavior.
package connections

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// defaultTokenTTL is the access-token lifetime assumed when a provider's
// token response carries no expires_in: a conservative default rather than
// treating the token as already expired, so on-demand refresh only ever
// fires once a token has genuinely gone stale.
const defaultTokenTTL = 1 * time.Hour

// tokenExpiryFrom computes an access token's expiry from now and the
// provider's own expires_in (seconds), falling back to defaultTokenTTL when
// the provider's response carried none (expiresInSeconds <= 0).
func tokenExpiryFrom(now time.Time, expiresInSeconds int) time.Time {
	ttl := defaultTokenTTL
	if expiresInSeconds > 0 {
		ttl = time.Duration(expiresInSeconds) * time.Second
	}
	return now.Add(ttl)
}

// needsRefresh reports whether c's stored access token must be refreshed
// before it can authorize a provider call: no expiry recorded at all (a
// Phase 1 row migrated with a NULL token_expires_at, self-healed on first
// use) or an expiry that has already passed.
func (c Connection) needsRefresh(now time.Time) bool {
	return c.TokenExpiresAt == nil || now.After(*c.TokenExpiresAt)
}

// needsProactiveRefresh reports whether c's stored access token is due for
// the refresh scheduler's own claim predicate (PD36): no expiry recorded at
// all, or an expiry within lead of now — earlier than needsRefresh's strict
// "already gone stale" check, which is the scheduler's whole point (refresh
// before expiry, not after).
func (c Connection) needsProactiveRefresh(now time.Time, lead time.Duration) bool {
	return c.TokenExpiresAt == nil || !c.TokenExpiresAt.After(now.Add(lead))
}

// refreshConnection performs one refresh_token grant for connection
// (assumed ACTIVE); deniedReason names the connection.expired reason a
// permanent refusal records (callers vary this — reconciliation's own
// revocation check records a different reason than a scheduled/request-path
// refusal). Callers reach this only through refreshOnce (refreshlock.go),
// which serializes concurrent callers per connection.
func (f *Facade) refreshConnection(ctx context.Context, connection Connection, deniedReason string) (Connection, error) {
	integration, err := f.integrations.GetIntegration(ctx, connection.IntegrationID)
	if err != nil {
		return Connection{}, err
	}
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil {
		return Connection{}, err
	}
	refreshToken, err := f.vault.Decrypt(connection.EncryptedRefreshToken)
	if err != nil {
		return Connection{}, err
	}

	request := RefreshGrantRequest{
		TokenURL:        definition.TokenURL,
		ClientID:        integration.ClientID,
		ClientSecret:    integration.ClientSecret,
		RefreshToken:    refreshToken,
		CredentialStyle: definition.CredentialStyle,
	}

	started := f.now()
	result, refreshErr := f.oauthClient.RefreshGrant(ctx, request)
	f.recordExchange(ctx, connection, started, refreshGrantRequestLogBody(request), result, refreshErr, true)

	if refreshErr != nil {
		return f.handleRefreshFailure(ctx, connection, refreshErr, deniedReason)
	}

	encryptedAccessToken, err := f.vault.Encrypt(result.AccessToken)
	if err != nil {
		return Connection{}, err
	}
	encryptedRefreshToken := connection.EncryptedRefreshToken
	if result.RefreshToken != "" {
		rotated, err := f.vault.Encrypt(result.RefreshToken)
		if err != nil {
			return Connection{}, err
		}
		encryptedRefreshToken = rotated
	}

	refreshed := connection.RefreshTokens(encryptedAccessToken, encryptedRefreshToken, tokenExpiryFrom(f.now(), result.ExpiresIn))
	return f.persistConnection(ctx, refreshed)
}

// handleRefreshFailure applies FD3's permanent-vs-transient split: only a
// RefreshDenied expires the connection (through expireConnection, recording
// deniedReason); any other error is transient and returned as-is.
func (f *Facade) handleRefreshFailure(ctx context.Context, connection Connection, refreshErr error, deniedReason string) (Connection, error) {
	var denied RefreshDenied
	if !errors.As(refreshErr, &denied) {
		return Connection{}, refreshErr
	}
	return f.expireConnection(ctx, connection, deniedReason)
}

// persistConnection saves connection via the repository and returns it
// unchanged, so refreshConnection's success outcome can share one
// persist-then-return line.
func (f *Facade) persistConnection(ctx context.Context, connection Connection) (Connection, error) {
	if err := f.repo.Update(ctx, connection); err != nil {
		return Connection{}, err
	}
	return connection, nil
}

// refreshGrantRequestLogBody builds a JSON representation of a refresh
// grant's request for logging (AC8), mirroring
// tokenExchangeRequestLogBody's shape for the authorization_code grant. It
// carries client_secret and the refresh token in cleartext — this stays in
// memory only; the logging module redacts every sensitive field before the
// entry is ever persisted (AC9's redaction rule, unchanged for this kind).
func refreshGrantRequestLogBody(request RefreshGrantRequest) string {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     request.ClientID,
		"client_secret": request.ClientSecret,
		"refresh_token": request.RefreshToken,
	})
	return string(body)
}
