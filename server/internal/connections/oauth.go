// oauth.go holds the OAuth handshake: the connect page's link to Microsoft's
// consent screen, the single-use CSRF state that binds the round trip back
// to exactly one connection attempt, and the callback's token exchange
// (Slice 4).
package connections

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"beecon/internal/catalog"
)

// ConnectLinkTTL is how long a connect-page link (the redirectUrl minted at
// Initiate) stays open before OpenConnectPage treats it as expired (AC2).
const ConnectLinkTTL = 1 * time.Hour

// OAuthStateTTL is how long a CSRF state minted by OpenConnectPage stays
// valid before HandleCallback treats it as expired (AC7).
const OAuthStateTTL = 10 * time.Minute

// OAuthState is the single-use CSRF token bound to one connection attempt:
// minted when the connect page is opened, consumed exactly once by the
// callback (AC3, AC7).
type OAuthState struct {
	State        string
	ConnectionID ConnectionID
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
}

// IsConsumed reports whether s has already been used by a previous callback.
func (s OAuthState) IsConsumed() bool {
	return s.ConsumedAt != nil
}

// IsExpired reports whether s's TTL has passed as of now.
func (s OAuthState) IsExpired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}

// ConnectPageView is what the connect-page driving adapter (connectweb)
// needs to render the provider's connect page: its name/logo and the
// Microsoft consent link, already carrying the Integration's client id, the
// provider definition's scopes, and a single-use CSRF state (AC1, AC3).
type ConnectPageView struct {
	ProviderName string
	ProviderLogo string
	AuthorizeURL string
}

// CallbackOutcome tells the connectweb driving adapter where to send the
// browser next: the consumer's redirectUri, carrying either a success status
// and the connection id (AC4) or an error status from a denied consent
// (AC8). HandleCallback returns an error instead of a CallbackOutcome for
// every case that must show an error page rather than redirect (AC7, AC9).
type CallbackOutcome struct {
	RedirectURL string
}

// OpenConnectPage validates the connect token — rejecting an invalid,
// expired, or already-completed connect link before it ever reaches the
// provider (AC2) — then mints a single-use CSRF state bound to this
// connection attempt and returns everything the connect page needs to
// render the Microsoft consent link (AC1, AC3).
func (f *Facade) OpenConnectPage(ctx context.Context, token string) (ConnectPageView, error) {
	connection, err := f.oauthRepo.FindByConnectToken(ctx, token)
	if err != nil {
		return ConnectPageView{}, err
	}
	if connection == nil {
		return ConnectPageView{}, ErrConnectLinkInvalid()
	}
	if connection.Status != StatusInitiated {
		return ConnectPageView{}, ErrConnectLinkAlreadyCompleted()
	}
	if f.now().Sub(connection.CreatedAt) > ConnectLinkTTL {
		return ConnectPageView{}, ErrConnectLinkExpired()
	}

	integration, err := f.integrations.GetIntegration(ctx, connection.IntegrationID)
	if err != nil {
		return ConnectPageView{}, err
	}
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil {
		return ConnectPageView{}, err
	}

	state := OAuthState{
		State:        f.newState(),
		ConnectionID: connection.ID,
		ExpiresAt:    f.now().Add(OAuthStateTTL),
	}
	if err := f.oauthRepo.SaveState(ctx, state); err != nil {
		return ConnectPageView{}, err
	}

	return ConnectPageView{
		ProviderName: definition.Name,
		ProviderLogo: definition.Logo,
		AuthorizeURL: buildAuthorizeURL(definition, integration.ClientID, f.baseURL, state.State),
	}, nil
}

// HandleCallback validates the CSRF state — a missing, unknown, expired, or
// already-used state shows an error page and never touches the connection
// (AC7) — then either forwards the user's denial to the consumer (AC8), or
// exchanges the code for tokens, captures the account profile, encrypts the
// tokens, and activates the connection under its original id (AC4, AC5, AC6,
// AC10). A token-exchange failure shows an error page and leaves the
// connection INITIATED (AC9, PD11).
func (f *Facade) HandleCallback(ctx context.Context, code, state, providerError string) (CallbackOutcome, error) {
	connection, err := f.consumeState(ctx, state)
	if err != nil {
		return CallbackOutcome{}, err
	}

	if providerError != "" {
		return CallbackOutcome{
			RedirectURL: buildConsumerRedirectURL(connection.RedirectURI, connection.ID, "error"),
		}, nil
	}

	activated, err := f.exchangeAndActivate(ctx, *connection, code)
	if err != nil {
		return CallbackOutcome{}, err
	}
	if err := f.repo.Update(ctx, activated); err != nil {
		return CallbackOutcome{}, err
	}

	return CallbackOutcome{
		RedirectURL: buildConsumerRedirectURL(activated.RedirectURI, activated.ID, "success"),
	}, nil
}

// consumeState validates state (AC7: missing, unknown, expired, or already
// used) and marks it consumed exactly once, returning the connection it was
// minted for.
func (f *Facade) consumeState(ctx context.Context, state string) (*Connection, error) {
	if state == "" {
		return nil, ErrStateMissing()
	}
	oauthState, err := f.oauthRepo.FindState(ctx, state)
	if err != nil {
		return nil, err
	}
	if oauthState == nil {
		return nil, ErrStateUnknown()
	}
	if oauthState.IsConsumed() {
		return nil, ErrStateAlreadyUsed()
	}
	if oauthState.IsExpired(f.now()) {
		return nil, ErrStateExpired()
	}
	if err := f.oauthRepo.MarkStateConsumed(ctx, state, f.now()); err != nil {
		return nil, err
	}

	connection, err := f.oauthRepo.FindConnectionForCallback(ctx, oauthState.ConnectionID)
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, ErrStateUnknown()
	}
	return connection, nil
}

// exchangeAndActivate exchanges code for tokens, fetches the account
// profile, and returns connection activated with the vault's ciphertext and
// the captured metadata. Any failure (exchange or account fetch) surfaces
// uniformly as ErrTokenExchangeFailed (AC9) — the connection this returns is
// never persisted on error. Every attempt — success or failure — writes one
// log entry (Slice 5, AC8) with the request/response bodies the token
// exchange sent and received; the logging module redacts them before
// persistence (AC9), so this function passes the raw values through
// in-memory only.
func (f *Facade) exchangeAndActivate(ctx context.Context, connection Connection, code string) (Connection, error) {
	integration, err := f.integrations.GetIntegration(ctx, connection.IntegrationID)
	if err != nil {
		return Connection{}, err
	}
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil {
		return Connection{}, err
	}

	request := TokenExchangeRequest{
		TokenURL:     definition.TokenURL,
		ClientID:     integration.ClientID,
		ClientSecret: integration.ClientSecret,
		Code:         code,
		RedirectURI:  buildCallbackURL(f.baseURL),
	}

	started := f.now()
	tokens, account, exchangeErr := f.exchangeTokensAndFetchAccount(ctx, request, definition.UserInfoURL)
	f.recordTokenExchange(ctx, connection, started, request, tokens, exchangeErr)
	if exchangeErr != nil {
		return Connection{}, ErrTokenExchangeFailed()
	}

	encryptedAccessToken, err := f.vault.Encrypt(tokens.AccessToken)
	if err != nil {
		return Connection{}, err
	}
	encryptedRefreshToken, err := f.vault.Encrypt(tokens.RefreshToken)
	if err != nil {
		return Connection{}, err
	}

	return connection.Activate(encryptedAccessToken, encryptedRefreshToken, account.Email, account.DisplayName), nil
}

// exchangeTokensAndFetchAccount performs the two upstream calls the token
// exchange needs in sequence: the authorization_code grant, then the
// account-profile fetch using the token it returned.
func (f *Facade) exchangeTokensAndFetchAccount(ctx context.Context, request TokenExchangeRequest, userInfoURL string) (TokenExchangeResult, AccountInfo, error) {
	tokens, err := f.oauthClient.ExchangeCode(ctx, request)
	if err != nil {
		return TokenExchangeResult{}, AccountInfo{}, err
	}
	account, err := f.oauthClient.FetchAccount(ctx, userInfoURL, tokens.AccessToken)
	if err != nil {
		return tokens, AccountInfo{}, err
	}
	return tokens, account, nil
}

// recordTokenExchange writes one log entry for a completed or failed token
// exchange (AC8). A nil recorder (no logging module wired) is a silent
// no-op; a recorder error never fails the OAuth handshake itself — logging
// is observability, not a precondition of the primary operation.
func (f *Facade) recordTokenExchange(ctx context.Context, connection Connection, started time.Time, request TokenExchangeRequest, tokens TokenExchangeResult, exchangeErr error) {
	if f.recorder == nil {
		return
	}
	status := http.StatusOK
	responseBody := tokenExchangeResponseLogBody(tokens)
	if exchangeErr != nil {
		status = http.StatusBadGateway
		responseBody = exchangeErr.Error()
	}
	_ = f.recorder.Record(ctx, LogEntry{
		OrgID:        connection.OrgID,
		UserID:       connection.UserID,
		ConnectionID: connection.ID,
		Status:       status,
		DurationMs:   f.now().Sub(started).Milliseconds(),
		RequestBody:  tokenExchangeRequestLogBody(request),
		ResponseBody: responseBody,
	})
}

// tokenExchangeRequestLogBody and tokenExchangeResponseLogBody build a JSON
// representation of the token exchange's request/response for logging
// (AC8). They carry client_secret and access/refresh tokens in cleartext —
// this stays in memory only; the logging module redacts every sensitive
// field before the entry is ever persisted (AC9).
func tokenExchangeRequestLogBody(request TokenExchangeRequest) string {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     request.ClientID,
		"client_secret": request.ClientSecret,
		"code":          request.Code,
		"redirect_uri":  request.RedirectURI,
	})
	return string(body)
}

func tokenExchangeResponseLogBody(tokens TokenExchangeResult) string {
	body, _ := json.Marshal(map[string]string{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
	})
	return string(body)
}

// buildAuthorizeURL builds the Microsoft consent link the connect page's
// Connect action points at: the Integration's client id, the provider
// definition's scopes, and the single-use CSRF state (AC3).
func buildAuthorizeURL(definition catalog.ProviderDefinition, clientID, baseURL, state string) string {
	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", buildCallbackURL(baseURL))
	query.Set("scope", strings.Join(definition.Scopes, " "))
	query.Set("state", state)
	return definition.AuthorizeURL + "?" + query.Encode()
}

// buildCallbackURL joins baseURL with the OAuth callback path every
// Integration's redirect_uri points at.
func buildCallbackURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/connect/oauth/callback"
}

// buildConsumerRedirectURL appends connectionId and status to redirectURI
// (AC4, AC8), preserving any query parameters the consumer's own redirectUri
// already carries.
func buildConsumerRedirectURL(redirectURI string, connectionID ConnectionID, status string) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	query := parsed.Query()
	query.Set("connectionId", string(connectionID))
	query.Set("status", status)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
