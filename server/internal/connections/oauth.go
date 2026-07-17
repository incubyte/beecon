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
	"regexp"
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
// provider definition's scopes, and a single-use CSRF state (AC1, AC3). When
// the provider declares expected pre-auth params and none have been
// submitted yet (Slice 3, AC3), ParamsRequired is true, ParamFields carries
// the fields the connect page's form must collect, and AuthorizeURL is
// empty — nothing is minted or forwarded to the provider until SubmitParams
// succeeds.
type ConnectPageView struct {
	ProviderName   string
	ProviderLogo   string
	AuthorizeURL   string
	ParamsRequired bool
	ParamFields    []catalog.ExpectedParam
}

// CallbackOutcome tells the connectweb driving adapter where to send the
// browser next: the consumer's redirectUri, carrying either a success status
// and the connection id (AC4) or an error status from a denied consent
// (AC8). HandleCallback returns an error instead of a CallbackOutcome for
// every case that must show an error page rather than redirect (AC7, AC9).
type CallbackOutcome struct {
	RedirectURL string
}

// validateConnectToken looks up the connect token's connection and rejects
// an invalid, expired, or already-completed connect link (AC2) before it
// ever reaches the provider — shared by OpenConnectPage and SubmitParams so
// both entry points into the connect flow enforce the same rules. The
// already-completed and expired checks key off ConnectTokenUsed and
// ConnectTokenExpiresAt rather than the connection's overall Status, so a
// Reconnect attempt (Slice 4, PD19) can run this exact handshake again
// without validateConnectToken mistaking a previously ACTIVE/EXPIRED/
// DISCONNECTED connection for one whose handshake already finished.
func (f *Facade) validateConnectToken(ctx context.Context, token string) (*Connection, error) {
	connection, err := f.oauthRepo.FindByConnectToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, ErrConnectLinkInvalid()
	}
	if connection.ConnectTokenUsed {
		return nil, ErrConnectLinkAlreadyCompleted()
	}
	if f.now().After(connection.ConnectTokenExpiresAt) {
		return nil, ErrConnectLinkExpired()
	}
	return connection, nil
}

// OpenConnectPage validates the connect token (AC2) and returns either the
// param-collection form — when the provider declares expected params and
// none have been submitted yet (Slice 3, AC3) — or the provider's consent
// link, minting the single-use CSRF state that binds it to this connection
// attempt (AC1, AC3).
func (f *Facade) OpenConnectPage(ctx context.Context, token string) (ConnectPageView, error) {
	connection, err := f.validateConnectToken(ctx, token)
	if err != nil {
		return ConnectPageView{}, err
	}
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil {
		return ConnectPageView{}, err
	}
	if needsParamsForm(definition, *connection) {
		return paramsFormView(definition), nil
	}
	return f.buildForwardingView(ctx, *connection, definition)
}

// SubmitParams validates the connect token exactly as OpenConnectPage does,
// then either rejects the submission with ErrMissingRequiredParams — naming
// every required field the caller's values left empty or absent, without
// persisting anything (AC4) — or stores the submitted values vault-encrypted
// (AC7) and returns the same forwarding view OpenConnectPage would return
// now that params are collected.
func (f *Facade) SubmitParams(ctx context.Context, token string, values map[string]string) (ConnectPageView, error) {
	connection, err := f.validateConnectToken(ctx, token)
	if err != nil {
		return ConnectPageView{}, err
	}
	definition, err := f.providers.GetProviderDefinition(ctx, connection.ProviderSlug)
	if err != nil {
		return ConnectPageView{}, err
	}
	if missing := missingRequiredParams(definition.ExpectedParams, values); len(missing) > 0 {
		return paramsFormView(definition), ErrMissingRequiredParams(missing)
	}
	encryptedParams, err := f.encryptParams(definition.ExpectedParams, values)
	if err != nil {
		return ConnectPageView{}, err
	}
	updated := connection.WithParams(encryptedParams)
	if err := f.repo.Update(ctx, updated); err != nil {
		return ConnectPageView{}, err
	}
	return f.buildForwardingView(ctx, updated, definition)
}

// needsParamsForm reports whether OpenConnectPage must show the
// param-collection form rather than forward to the provider: the definition
// declares at least one expected param, and none have been submitted yet.
func needsParamsForm(definition catalog.ProviderDefinition, connection Connection) bool {
	return len(definition.ExpectedParams) > 0 && connection.EncryptedParams == ""
}

// paramsFormView builds the ConnectPageView connectweb renders as the
// param-collection form (AC3): no state is minted and no AuthorizeURL is
// built until the required fields are collected.
func paramsFormView(definition catalog.ProviderDefinition) ConnectPageView {
	return ConnectPageView{
		ProviderName:   definition.Name,
		ProviderLogo:   definition.Logo,
		ParamsRequired: true,
		ParamFields:    definition.ExpectedParams,
	}
}

// buildForwardingView mints the single-use CSRF state bound to connection
// and returns the Microsoft consent link (AC1, AC3), decrypting any
// previously collected params so they are usable in the provider's own
// authorize URL via {params.x} templating (AC8).
func (f *Facade) buildForwardingView(ctx context.Context, connection Connection, definition catalog.ProviderDefinition) (ConnectPageView, error) {
	integration, err := f.integrations.GetIntegration(ctx, connection.IntegrationID)
	if err != nil {
		return ConnectPageView{}, err
	}
	params, err := f.decryptParams(connection.EncryptedParams)
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
		AuthorizeURL: buildAuthorizeURL(definition, integration.ClientID, f.baseURL, state.State, params),
	}, nil
}

// missingRequiredParams returns the name of every ExpectedParam marked
// Required whose value is empty or absent from values (AC4). Optional
// params are never checked.
func missingRequiredParams(fields []catalog.ExpectedParam, values map[string]string) []string {
	var missing []string
	for _, field := range fields {
		if !field.Required {
			continue
		}
		if strings.TrimSpace(values[field.Name]) == "" {
			missing = append(missing, field.Name)
		}
	}
	return missing
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
	params, err := f.decryptParams(connection.EncryptedParams)
	if err != nil {
		return Connection{}, err
	}

	request := TokenExchangeRequest{
		TokenURL:        renderParamsTemplate(definition.TokenURL, params),
		ClientID:        integration.ClientID,
		ClientSecret:    integration.ClientSecret,
		Code:            code,
		RedirectURI:     buildCallbackURL(f.baseURL),
		CredentialStyle: definition.CredentialStyle,
	}

	started := f.now()
	tokens, account, exchangeErr := f.exchangeTokensAndFetchAccount(ctx, request, definition)
	f.recordExchange(ctx, connection, started, tokenExchangeRequestLogBody(request), tokens, exchangeErr, false)
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

	tokenExpiresAt := tokenExpiryFrom(f.now(), tokens.ExpiresIn)
	return connection.Activate(encryptedAccessToken, encryptedRefreshToken, account.Email, account.DisplayName, tokenExpiresAt), nil
}

// exchangeTokensAndFetchAccount performs the two upstream calls the token
// exchange needs in sequence: the authorization_code grant, then the
// account-profile fetch using the token it returned, read via the
// definition's own declared userInfo field mapping (PD13) — generic across
// providers, so Hubspot's differently-shaped token-metadata response needs no
// provider-specific Go code here (AC1). A definition with no userInfo block
// (empty UserInfoURL) skips the account-profile fetch entirely and activates
// with no captured identity, mirroring reconcileOne's guard (reconcile.go) —
// otherwise FetchAccount would be called against an empty URL.
func (f *Facade) exchangeTokensAndFetchAccount(ctx context.Context, request TokenExchangeRequest, definition catalog.ProviderDefinition) (TokenExchangeResult, AccountInfo, error) {
	tokens, err := f.oauthClient.ExchangeCode(ctx, request)
	if err != nil {
		return TokenExchangeResult{}, AccountInfo{}, err
	}
	if definition.UserInfoURL == "" {
		return tokens, AccountInfo{}, nil
	}
	account, err := f.oauthClient.FetchAccount(ctx, AccountFetchRequest{
		UserInfoURL:      definition.UserInfoURL,
		AccessToken:      tokens.AccessToken,
		EmailField:       definition.UserInfo.EmailField,
		DisplayNameField: definition.UserInfo.DisplayNameField,
	})
	if err != nil {
		return tokens, AccountInfo{}, err
	}
	return tokens, account, nil
}

// recordExchange writes one log entry for a completed or failed OAuth grant
// — the authorization_code exchange (oauth.go) or a refresh_token grant
// (refresh.go, PD18) — under the same Recorder port (AC8; Slice 4's refresh
// reuses it as an oauth_token_exchange entry too, so the same code/token
// redaction applies), and records the matching PD24 metric (Slice 6):
// isRefresh routes to the token-refresh counter instead of the OAuth
// handshake counter, even though both share one Recorder kind. A nil
// recorder (no logging module wired) is a silent no-op; a recorder error
// never fails the operation itself — logging is observability, not a
// precondition of the primary operation.
func (f *Facade) recordExchange(ctx context.Context, connection Connection, started time.Time, requestBody string, tokens TokenExchangeResult, exchangeErr error, isRefresh bool) {
	f.recordExchangeMetric(connection.ProviderSlug, isRefresh, exchangeErr == nil)
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
		RequestBody:  requestBody,
		ResponseBody: responseBody,
	})
}

// recordExchangeMetric records PD24's OAuth handshake/token-refresh outcome
// counters (Slice 6).
func (f *Facade) recordExchangeMetric(providerSlug string, isRefresh, success bool) {
	if f.metrics == nil {
		return
	}
	if isRefresh {
		f.metrics.RecordTokenRefresh(providerSlug, success)
		return
	}
	f.metrics.RecordOAuthHandshake(providerSlug, success)
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
// definition's scopes, the single-use CSRF state (AC3), and — when the
// provider declared expected params — their collected values, templated
// into the definition's own AuthorizeURL via {params.x} (Slice 3, AC8).
func buildAuthorizeURL(definition catalog.ProviderDefinition, clientID, baseURL, state string, params map[string]string) string {
	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", buildCallbackURL(baseURL))
	query.Set("scope", strings.Join(definition.Scopes, " "))
	query.Set("state", state)
	return renderParamsTemplate(definition.AuthorizeURL, params) + "?" + query.Encode()
}

// paramTemplatePattern matches a {params.x} token (Slice 3, AC8).
var paramTemplatePattern = regexp.MustCompile(`\{params\.([A-Za-z0-9_]+)\}`)

// renderParamsTemplate substitutes every {params.x} token in template with
// its value from params — the same substitution execution/template.go
// applies to tool mappings, kept as its own small copy here because
// connections cannot import execution (BOUNDARIES: execution depends on
// connections, not the reverse). A token whose param was not supplied is
// left untouched: SubmitParams already rejects any submission missing a
// required param before a connection ever reaches this path, so this only
// ever fires for an optional param the caller left out.
func renderParamsTemplate(template string, params map[string]string) string {
	return paramTemplatePattern.ReplaceAllStringFunc(template, func(token string) string {
		name := paramTemplatePattern.FindStringSubmatch(token)[1]
		if value, ok := params[name]; ok {
			return value
		}
		return token
	})
}

// encryptParams keeps only the values named by fields (dropping anything
// else the connect page's form posted), JSON-encodes them, and vault-
// encrypts the whole blob (PD17) — a connection's collected pre-auth param
// values are never persisted in plaintext, the same rule as its OAuth
// tokens. Returns "" when fields declares no params or none were supplied.
func (f *Facade) encryptParams(fields []catalog.ExpectedParam, values map[string]string) (string, error) {
	filtered := make(map[string]string, len(fields))
	for _, field := range fields {
		if value, ok := values[field.Name]; ok {
			filtered[field.Name] = value
		}
	}
	if len(filtered) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(filtered)
	if err != nil {
		return "", err
	}
	return f.vault.Encrypt(string(encoded))
}

// decryptParams reverses encryptParams, returning nil for a connection that
// carries no collected params.
func (f *Facade) decryptParams(encryptedParams string) (map[string]string, error) {
	if encryptedParams == "" {
		return nil, nil
	}
	plaintext, err := f.vault.Decrypt(encryptedParams)
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(plaintext), &values); err != nil {
		return nil, err
	}
	return values, nil
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
