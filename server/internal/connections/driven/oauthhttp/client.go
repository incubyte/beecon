// Package oauthhttp is the connections module's real driven OAuthClient: a
// single form-POST authorization_code exchange (stdlib net/http, no
// x/oauth2 — Beecon's OAuth flow is one custom exchange, not a general
// client) and a bearer-authenticated GET for the account profile.
package oauthhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"beecon/internal/connections"
)

// defaultTimeout bounds every request this client makes to a provider —
// neither the token endpoint nor the user-info endpoint should ever hang the
// OAuth callback indefinitely.
const defaultTimeout = 15 * time.Second

// Client is the connections module's real driven OAuthClient.
type Client struct {
	httpClient *http.Client
}

var _ connections.OAuthClient = (*Client)(nil)

// NewClient builds a Client. A nil httpClient falls back to one with
// defaultTimeout.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

// tokenExchangeResponse is a token endpoint's JSON shape, shared by the
// authorization_code and refresh_token grants: ExpiresIn (PD18) is the
// access token's lifetime in seconds, absent (0) when a provider doesn't
// report one.
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeCode completes the authorization_code grant against req.TokenURL:
// one form-POST carrying the code the provider issued, plus the
// Integration's client id/secret in whichever place req.CredentialStyle
// declares (PD13) — the form body (Outlook's and Hubspot's shared behavior,
// and the default when unset) or an HTTP Basic Authorization header.
func (c *Client) ExchangeCode(ctx context.Context, req connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("redirect_uri", req.RedirectURI)

	status, body, err := c.doTokenGrant(ctx, req.TokenURL, form, req.CredentialStyle, req.ClientID, req.ClientSecret)
	if err != nil {
		return connections.TokenExchangeResult{}, err
	}
	if status != http.StatusOK {
		return connections.TokenExchangeResult{}, fmt.Errorf("token endpoint returned status %d", status)
	}
	return decodeTokenExchangeResponse(body)
}

// RefreshGrant completes a refresh_token grant against req.TokenURL (PD18).
// A non-200 response naming one of permanentRefreshErrorCodes (FD3) returns
// a typed connections.RefreshDenied; any other failure is transient.
func (c *Client) RefreshGrant(ctx context.Context, req connections.RefreshGrantRequest) (connections.TokenExchangeResult, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", req.RefreshToken)

	status, body, err := c.doTokenGrant(ctx, req.TokenURL, form, req.CredentialStyle, req.ClientID, req.ClientSecret)
	if err != nil {
		return connections.TokenExchangeResult{}, err
	}
	if status != http.StatusOK {
		if code, denied := permanentRefreshErrorCode(body); denied {
			return connections.TokenExchangeResult{}, connections.RefreshDenied{OAuthErrorCode: code}
		}
		return connections.TokenExchangeResult{}, fmt.Errorf("token endpoint returned status %d", status)
	}
	return decodeTokenExchangeResponse(body)
}

// doTokenGrant executes one token-endpoint round trip and returns the raw
// HTTP status and body; err is non-nil only for a network-level failure —
// callers interpret a non-200 status themselves.
func (c *Client) doTokenGrant(ctx context.Context, tokenURL string, form url.Values, credentialStyle, clientID, clientSecret string) (status int, body []byte, err error) {
	if credentialStyle != connections.CredentialStyleBasicAuth {
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("build token grant request: %w", err)
	}
	if credentialStyle == connections.CredentialStyleBasicAuth {
		httpReq.SetBasicAuth(clientID, clientSecret)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read token endpoint response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// permanentRefreshErrorCodes are PD36's "invalid_grant and kin" (FD3).
var permanentRefreshErrorCodes = map[string]bool{
	"invalid_grant":       true,
	"invalid_client":      true,
	"unauthorized_client": true,
}

// permanentRefreshErrorCode reads a token endpoint's OAuth-shaped error body
// ({"error": "..."}), reporting whether its code is a permanent refusal.
func permanentRefreshErrorCode(body []byte) (string, bool) {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return "", false
	}
	if !permanentRefreshErrorCodes[parsed.Error] {
		return "", false
	}
	return parsed.Error, true
}

func decodeTokenExchangeResponse(body []byte) (connections.TokenExchangeResult, error) {
	var decoded tokenExchangeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return connections.TokenExchangeResult{}, fmt.Errorf("decode token endpoint response: %w", err)
	}
	if decoded.AccessToken == "" {
		return connections.TokenExchangeResult{}, errors.New("token endpoint response carried no access_token")
	}
	return tokenExchangeResultFrom(decoded), nil
}

func tokenExchangeResultFrom(body tokenExchangeResponse) connections.TokenExchangeResult {
	return connections.TokenExchangeResult{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresIn:    body.ExpiresIn,
	}
}

// accessTokenURLPlaceholder is a userInfoUrl token a provider definition may
// carry when its user-info endpoint needs the access token embedded in the
// URL itself rather than (or in addition to) a bearer header — Hubspot's
// token-metadata endpoint, GET /oauth/v1/access-tokens/{token}, is the
// motivating case (PD13, Slice 2).
const accessTokenURLPlaceholder = "{accessToken}"

// FetchAccount fetches the authenticated account's profile via a
// bearer-authenticated GET to req.UserInfoURL (substituting
// accessTokenURLPlaceholder when present), then reads the email and display
// name out of the decoded JSON response generically, by the field names
// req.EmailField/req.DisplayNameField name (PD13's userInfo mapping) — this
// is what lets Hubspot's token-metadata response ({"user":..., "hub_domain":
// ...}) reuse this same driven adapter Outlook's GET /v1.0/me already uses,
// with no provider-specific Go code (AC1).
func (c *Client) FetchAccount(ctx context.Context, req connections.AccountFetchRequest) (connections.AccountInfo, error) {
	userInfoURL := strings.ReplaceAll(req.UserInfoURL, accessTokenURLPlaceholder, url.PathEscape(req.AccessToken))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return connections.AccountInfo{}, fmt.Errorf("build account info request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.AccessToken)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return connections.AccountInfo{}, fmt.Errorf("call user-info endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return connections.AccountInfo{}, fmt.Errorf("user-info endpoint returned status %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return connections.AccountInfo{}, fmt.Errorf("decode account info response: %w", err)
	}

	return connections.AccountInfo{
		Email:       stringField(body, req.EmailField),
		DisplayName: stringField(body, req.DisplayNameField),
	}, nil
}

// FetchUserInfo performs PD37's reconciliation probe: a bearer-authenticated
// GET against userInfoURL, discarding the response body. A 401 returns
// connections.ErrProbeUnauthorized (FD9's only evidence of revocation); any
// other non-2xx or network failure returns a plain error.
func (c *Client) FetchUserInfo(ctx context.Context, userInfoURL, accessToken string) error {
	resolvedURL := strings.ReplaceAll(userInfoURL, accessTokenURLPlaceholder, url.PathEscape(accessToken))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return fmt.Errorf("build reconciliation probe request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call reconciliation probe endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return connections.ErrProbeUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("reconciliation probe endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// stringField reads field out of body as a string, or "" when field is
// unset, absent, or not a string value.
func stringField(body map[string]any, field string) string {
	if field == "" {
		return ""
	}
	value, _ := body[field].(string)
	return value
}
