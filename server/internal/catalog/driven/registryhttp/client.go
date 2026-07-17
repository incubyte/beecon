// Package registryhttp is catalog.RegistryClient's HTTP adapter (PD64): it
// calls the separate registry service's pull API over
// BEECON_REGISTRY_URL, authenticated with BEECON_REGISTRY_API_KEY (PD67:
// v1 trust is API-key auth + TLS; content-hash verification on activation
// is a later slice). Every non-success response collapses to catalog's own
// errors so callers never depend on this adapter's shape.
package registryhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"beecon/internal/catalog"
	"beecon/internal/registrybundle"
)

// Client is catalog.RegistryClient's HTTP adapter. A nil httpClient falls
// back to http.DefaultClient, mirroring
// execution/driven/providerhttp.Client's own convention.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

var _ catalog.RegistryClient = (*Client)(nil)

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: httpClient}
}

// FetchBundle calls
// GET {baseURL}/registry/v1/providers/{providerSlug}/bundles/{version},
// bearer-authenticated. A 404 is catalog.ErrBundleVersionNotFound;
// anything else that isn't a clean 200 (network failure, 401, 5xx, an
// undecodable body) is catalog.ErrRegistryUnavailable — Slice 1 keeps this
// deliberately coarse; Slice 3/4 may distinguish these further as their own
// ACs require it.
func (c *Client) FetchBundle(ctx context.Context, providerSlug, version string) (registrybundle.Bundle, error) {
	url := fmt.Sprintf("%s/registry/v1/providers/%s/bundles/%s", c.baseURL, providerSlug, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return registrybundle.Bundle{}, catalog.ErrRegistryUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return registrybundle.Bundle{}, catalog.ErrRegistryUnavailable()
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var bundle registrybundle.Bundle
		if err := json.NewDecoder(resp.Body).Decode(&bundle); err != nil {
			return registrybundle.Bundle{}, catalog.ErrRegistryUnavailable()
		}
		return bundle, nil
	case http.StatusNotFound:
		return registrybundle.Bundle{}, catalog.ErrBundleVersionNotFound()
	default:
		return registrybundle.Bundle{}, catalog.ErrRegistryUnavailable()
	}
}

// listVersionsResponse mirrors the registry's own
// GET .../bundles response envelope (registryservice/driving/httpapi's
// bundleVersionsDTO) — only the version field this adapter's caller needs
// (Slice 3: an operator's version-list/diff review), decoded independently
// rather than importing registryservice's own DTO type across the
// installation/registry deployable boundary.
type listVersionsResponse struct {
	Items []struct {
		Version string `json:"version"`
	} `json:"items"`
}

// ListVersions calls
// GET {baseURL}/registry/v1/providers/{providerSlug}/bundles, bearer-
// authenticated (Slice 3): every version this provider has published, for
// an installation operator to review before pulling/activating one.
// Network failure, a non-200 response, or an undecodable body all collapse
// to catalog.ErrRegistryUnavailable, mirroring FetchBundle's own
// deliberately coarse error handling.
func (c *Client) ListVersions(ctx context.Context, providerSlug string) ([]string, error) {
	url := fmt.Sprintf("%s/registry/v1/providers/%s/bundles", c.baseURL, providerSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, catalog.ErrRegistryUnavailable()
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, catalog.ErrRegistryUnavailable()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, catalog.ErrRegistryUnavailable()
	}
	var body listVersionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, catalog.ErrRegistryUnavailable()
	}
	versions := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		versions = append(versions, item.Version)
	}
	return versions, nil
}
