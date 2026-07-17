// list_versions_http_test.go (package httpapi_test, alongside
// registry_service_http_test.go — same in-memory Store + real
// httptest.Server convention). Exercises GET
// /registry/v1/providers/{providerSlug}/bundles (Slice 3): the same
// installation-facing API-key trust boundary Pull already guards (PD67).
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/registryservice"
	"beecon/internal/registryservice/driven/memory"
	"beecon/internal/registryservice/driving/httpapi"
)

// newTestRegistryHTTPServerWithListVersions mirrors newTestRegistryHTTPServer
// but additionally mounts GET .../bundles (ListVersions), exactly as
// cmd/registry/main.go's real buildRouter does — publish, pull, and
// list-versions all guarded by their respective bearer tokens.
func newTestRegistryHTTPServerWithListVersions(t *testing.T) *httptest.Server {
	t.Helper()
	store := memory.NewStore()
	facade := registryservice.NewFacade(store, idgen.Prefixed("tool_"), func() time.Time { return time.Now().UTC() })
	handler := httpapi.NewHandler(facade, httpx.NewErrorRenderer(nil))

	r := chi.NewRouter()
	r.Route("/registry/v1/providers/{providerSlug}/bundles", func(r chi.Router) {
		r.With(httpapi.RequireBearerToken(testPublishToken)).Post("/", handler.Publish)
		r.With(httpapi.RequireBearerToken(testAPIKey)).Get("/", handler.ListVersions)
		r.With(httpapi.RequireBearerToken(testAPIKey)).Get("/{version}", handler.Pull)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

func doListVersions(t *testing.T, server *httptest.Server, bearerToken string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/registry/v1/providers/outlook/bundles", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestListVersions_WithAMissingAPIKeyIsRejected(t *testing.T) {
	server := newTestRegistryHTTPServerWithListVersions(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishResp.Body.Close()

	resp := doListVersions(t, server, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestListVersions_WithTheCorrectAPIKeyReturnsEveryPublishedVersion(t *testing.T) {
	server := newTestRegistryHTTPServerWithListVersions(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishBody, _ := io.ReadAll(publishResp.Body)
	publishResp.Body.Close()
	var published struct {
		Version     string `json:"version"`
		ContentHash string `json:"contentHash"`
	}
	if err := json.Unmarshal(publishBody, &published); err != nil {
		t.Fatalf("decode publish response: %v; body=%s", err, publishBody)
	}

	resp := doListVersions(t, server, testAPIKey)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, body)
	}
	var page struct {
		Items []struct {
			Version     string `json:"version"`
			ContentHash string `json:"contentHash"`
			PublishedAt string `json:"publishedAt"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode list-versions response: %v; body=%s", err, body)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %+v, want exactly the 1 published version", page.Items)
	}
	if page.Items[0].Version != published.Version {
		t.Errorf("Version = %q, want %q", page.Items[0].Version, published.Version)
	}
	if page.Items[0].ContentHash != published.ContentHash {
		t.Errorf("ContentHash = %q, want %q", page.Items[0].ContentHash, published.ContentHash)
	}
	if page.Items[0].PublishedAt == "" {
		t.Errorf("PublishedAt must be set, got empty")
	}
}

func TestListVersions_AnUnpublishedProviderReturnsAnEmptyItemsList(t *testing.T) {
	server := newTestRegistryHTTPServerWithListVersions(t)

	resp := doListVersions(t, server, testAPIKey)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, body)
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode list-versions response: %v; body=%s", err, body)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items = %+v, want empty for a provider that has never published", page.Items)
	}
}
