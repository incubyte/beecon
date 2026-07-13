// refresh.go implements PD18's on-demand token refresh: connections stores
// each ACTIVE connection's access-token expiry, and refreshes it exactly
// once per Resolve/RefreshForExecution call when needed, via the provider's
// refresh_token grant. A rotated refresh token replaces the stored one; a
// refresh grant the provider rejects (e.g. a revoked refresh token)
// transitions the connection to EXPIRED instead of leaving it ACTIVE with a
// token that will never work again (AC9).
package connections

import (
	"context"
	"encoding/json"
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

// refreshConnection performs one refresh_token grant for connection
// (assumed ACTIVE): it replaces the stored access token, replaces the stored
// refresh token only when the provider rotated it (AC8), and updates the
// access token's expiry. A grant the provider rejects transitions the
// connection to EXPIRED instead (AC9). Either outcome is persisted before it
// is returned to the caller.
func (f *Facade) refreshConnection(ctx context.Context, connection Connection) (Connection, error) {
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
		return f.persistConnection(ctx, connection.MarkExpired())
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

// persistConnection saves connection via the repository and returns it
// unchanged, so refreshConnection's two outcomes (refreshed or expired) can
// share one persist-then-return line.
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
