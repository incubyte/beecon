// Package providerhttp is the execution module's real driven ProviderClient:
// one bearer-authenticated HTTP call per tool execution (stdlib net/http —
// Beecon's tool call is a single request, not a general API client).
package providerhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"beecon/internal/execution"
)

// defaultTimeout bounds every request this client makes to a provider — a
// tool execution should never hang the caller indefinitely.
const defaultTimeout = 15 * time.Second

// Client is the execution module's real driven ProviderClient.
type Client struct {
	httpClient *http.Client
}

var _ execution.ProviderClient = (*Client)(nil)

// NewClient builds a Client. A nil httpClient falls back to one with
// defaultTimeout.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

// Call makes one bearer-authenticated HTTP request to a provider's tool
// endpoint (PD8: GET /v1.0/me/messages on Microsoft Graph, with
// top/skip/select/filter carried as query parameters). It returns an error
// only when the provider could not be reached at all; a non-2xx response is
// returned as a normal ToolCallResponse so Execute can turn it into AC7's
// tool-level failure.
func (c *Client) Call(ctx context.Context, req execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	requestURL, err := buildRequestURL(req.URL, req.Query)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("build tool call url: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, requestURL, nil)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("build tool call request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.AccessToken)
	httpReq.Header.Set("Accept", "application/json")
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("read provider response: %w", err)
	}
	return execution.ToolCallResponse{StatusCode: resp.StatusCode, Body: string(body)}, nil
}

// buildRequestURL appends query onto rawURL's own query string (rawURL is
// the tool's full call URL from its provider definition).
func buildRequestURL(rawURL string, query map[string]string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}
