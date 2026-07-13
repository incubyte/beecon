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

	body, err := c.doTokenGrant(ctx, req.TokenURL, form, req.CredentialStyle, req.ClientID, req.ClientSecret)
	if err != nil {
		return connections.TokenExchangeResult{}, err
	}
	return tokenExchangeResultFrom(body), nil
}

// RefreshGrant completes a refresh_token grant against req.TokenURL (PD18):
// the same credential-style-aware client authentication ExchangeCode uses,
// carrying a stored refresh token instead of a fresh authorization code. A
// response with an empty refresh_token means the provider did not rotate
// it — the caller keeps the one it already has (AC8's other branch).
func (c *Client) RefreshGrant(ctx context.Context, req connections.RefreshGrantRequest) (connections.TokenExchangeResult, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", req.RefreshToken)

	body, err := c.doTokenGrant(ctx, req.TokenURL, form, req.CredentialStyle, req.ClientID, req.ClientSecret)
	if err != nil {
		return connections.TokenExchangeResult{}, err
	}
	return tokenExchangeResultFrom(body), nil
}

// doTokenGrant executes one token-endpoint round trip: form already carries
// the grant's own fields (grant_type plus either code/redirect_uri or
// refresh_token); credentialStyle decides whether clientID/clientSecret ride
// in form or an HTTP Basic Authorization header (PD13) — shared by
// ExchangeCode's authorization_code grant and RefreshGrant's refresh_token
// grant, which differ only in which grant fields the form carries.
func (c *Client) doTokenGrant(ctx context.Context, tokenURL string, form url.Values, credentialStyle, clientID, clientSecret string) (tokenExchangeResponse, error) {
	if credentialStyle != connections.CredentialStyleBasicAuth {
		form.Set("client_id", clientID)
		form.Set("client_secret", clientSecret)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("build token grant request: %w", err)
	}
	if credentialStyle == connections.CredentialStyleBasicAuth {
		httpReq.SetBasicAuth(clientID, clientSecret)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tokenExchangeResponse{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var body tokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return tokenExchangeResponse{}, fmt.Errorf("decode token endpoint response: %w", err)
	}
	if body.AccessToken == "" {
		return tokenExchangeResponse{}, errors.New("token endpoint response carried no access_token")
	}
	return body, nil
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

// stringField reads field out of body as a string, or "" when field is
// unset, absent, or not a string value.
func stringField(body map[string]any, field string) string {
	if field == "" {
		return ""
	}
	value, _ := body[field].(string)
	return value
}
