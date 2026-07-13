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

// tokenExchangeResponse is the authorization_code grant's JSON shape.
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
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
	if req.CredentialStyle != connections.CredentialStyleBasicAuth {
		form.Set("client_id", req.ClientID)
		form.Set("client_secret", req.ClientSecret)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return connections.TokenExchangeResult{}, fmt.Errorf("build token exchange request: %w", err)
	}
	if req.CredentialStyle == connections.CredentialStyleBasicAuth {
		httpReq.SetBasicAuth(req.ClientID, req.ClientSecret)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return connections.TokenExchangeResult{}, fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return connections.TokenExchangeResult{}, fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var body tokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return connections.TokenExchangeResult{}, fmt.Errorf("decode token exchange response: %w", err)
	}
	if body.AccessToken == "" {
		return connections.TokenExchangeResult{}, errors.New("token exchange response carried no access_token")
	}
	return connections.TokenExchangeResult{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
	}, nil
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
