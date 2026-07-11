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
// one form-POST carrying the Integration's client id/secret and the code
// Microsoft issued.
func (c *Client) ExchangeCode(ctx context.Context, req connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("client_id", req.ClientID)
	form.Set("client_secret", req.ClientSecret)
	form.Set("redirect_uri", req.RedirectURI)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return connections.TokenExchangeResult{}, fmt.Errorf("build token exchange request: %w", err)
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

// meResponse is Microsoft Graph's GET /v1.0/me shape (the fields Beecon
// needs from it): mail is nullable for some tenant configurations, so
// FetchAccount falls back to userPrincipalName.
type meResponse struct {
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	DisplayName       string `json:"displayName"`
}

// FetchAccount fetches the authenticated account's profile via a
// bearer-authenticated GET to userInfoURL (PD9: Microsoft Graph's
// GET /v1.0/me for Outlook).
func (c *Client) FetchAccount(ctx context.Context, userInfoURL, accessToken string) (connections.AccountInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return connections.AccountInfo{}, fmt.Errorf("build account info request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return connections.AccountInfo{}, fmt.Errorf("call user-info endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return connections.AccountInfo{}, fmt.Errorf("user-info endpoint returned status %d", resp.StatusCode)
	}

	var body meResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return connections.AccountInfo{}, fmt.Errorf("decode account info response: %w", err)
	}

	email := body.Mail
	if email == "" {
		email = body.UserPrincipalName
	}
	return connections.AccountInfo{Email: email, DisplayName: body.DisplayName}, nil
}
