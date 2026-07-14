// Package webhookhttp is the delivery module's real driven EndpointCaller:
// a dumb POST to a consumer's webhook receiver (stdlib net/http).
package webhookhttp

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"beecon/internal/delivery"
)

// Client is the delivery module's real driven EndpointCaller.
type Client struct {
	httpClient *http.Client
}

var _ delivery.EndpointCaller = (*Client)(nil)

// NewClient builds a Client. A nil httpClient falls back to a fresh
// http.Client{} — Post always bounds the request with its own per-call
// timeout via a context deadline (BEECON_DELIVERY_TIMEOUT), so the
// underlying client needs no Timeout of its own.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{httpClient: httpClient}
}

// Post makes one POST request to url, carrying headers and body, bounded
// by timeout. It returns an error only when the endpoint could not be
// reached at all (including a timeout) — a response that was received at
// all, even a non-2xx one, returns (status, nil): Standard Webhooks'
// success rule (PD30: 2xx) lives in the caller (delivery.Facade), not
// here.
func (c *Client) Post(ctx context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}
