// Package httpapi tests exercise the registry service's own HTTP surface —
// publish and pull, each guarded by its own bearer credential (PD60/PD63 vs
// PD67) — through a real httptest.Server wrapping the same two routes
// cmd/registry/main.go's buildRouter mounts, backed by the in-memory Store so
// no filesystem/git adapter is needed for these tests.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/registryservice"
	"beecon/internal/registryservice/driven/memory"
	"beecon/internal/registryservice/driving/httpapi"
)

const (
	testPublishToken = "the-publish-token"
	testAPIKey       = "the-installation-api-key"

	oneToolBundleJSON = `{
		"formatVersion": 1,
		"name": "Outlook",
		"tools": [{"slug": "outlook-list-messages", "name": "List messages", "inputSchema": {"type":"object"}, "outputSchema": {"type":"object"}, "sample": {"status":"ok"}}]
	}`
)

func newTestRegistryHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	store := memory.NewStore()
	facade := registryservice.NewFacade(store, idgen.Prefixed("tool_"), func() time.Time { return time.Now().UTC() })
	handler := httpapi.NewHandler(facade, httpx.NewErrorRenderer(nil))

	r := chi.NewRouter()
	r.Route("/registry/v1/providers/{providerSlug}/bundles", func(r chi.Router) {
		r.With(httpapi.RequireBearerToken(testPublishToken)).Post("/", handler.Publish)
		r.With(httpapi.RequireBearerToken(testAPIKey)).Get("/{version}", handler.Pull)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

func doPublish(t *testing.T, server *httptest.Server, bearerToken string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/registry/v1/providers/outlook/bundles", strings.NewReader(oneToolBundleJSON))
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

func doPull(t *testing.T, server *httptest.Server, version, bearerToken string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/registry/v1/providers/outlook/bundles/"+version, nil)
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

func TestPublish_WithAMissingPublishTokenIsRejected(t *testing.T) {
	server := newTestRegistryHTTPServer(t)

	resp := doPublish(t, server, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestPublish_WithTheWrongPublishTokenIsRejected(t *testing.T) {
	server := newTestRegistryHTTPServer(t)

	resp := doPublish(t, server, "not-the-real-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestPublish_WithTheCorrectPublishTokenSucceeds(t *testing.T) {
	server := newTestRegistryHTTPServer(t)

	resp := doPublish(t, server, testPublishToken)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusCreated, body)
	}
}

func TestPull_WithAMissingAPIKeyIsRejectedAndReturnsNoBundleData(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishResp.Body.Close()

	resp := doPull(t, server, "1.0.0", "")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if strings.Contains(string(body), "outlook-list-messages") {
		t.Errorf("an unauthorized pull must return no bundle data at all, got: %s", body)
	}
}

func TestPull_WithAnInvalidAPIKeyIsRejected(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishResp.Body.Close()

	resp := doPull(t, server, "1.0.0", "wrong-key")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestPull_WithTheCorrectAPIKeyReturnsTheFullBundleIncludingToolIDsAndSchemas(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishBody, _ := io.ReadAll(publishResp.Body)
	publishResp.Body.Close()
	var published struct {
		Version string `json:"version"`
		Tools   []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(publishBody, &published); err != nil {
		t.Fatalf("decode publish response: %v; body=%s", err, publishBody)
	}

	resp := doPull(t, server, published.Version, testAPIKey)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusOK, body)
	}
	var pulled struct {
		ProviderSlug string `json:"providerSlug"`
		Tools        []struct {
			ID           string         `json:"id"`
			Slug         string         `json:"slug"`
			InputSchema  map[string]any `json:"inputSchema"`
			OutputSchema map[string]any `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &pulled); err != nil {
		t.Fatalf("decode pull response: %v; body=%s", err, body)
	}
	if pulled.ProviderSlug != "outlook" {
		t.Errorf("providerSlug = %q, want %q", pulled.ProviderSlug, "outlook")
	}
	if len(pulled.Tools) != 1 {
		t.Fatalf("tools = %+v, want exactly 1", pulled.Tools)
	}
	if pulled.Tools[0].ID != published.Tools[0].ID {
		t.Errorf("pulled tool id = %q, want the published id %q", pulled.Tools[0].ID, published.Tools[0].ID)
	}
	if pulled.Tools[0].InputSchema == nil || pulled.Tools[0].OutputSchema == nil {
		t.Errorf("pulled tool must carry its input/output schemas: %+v", pulled.Tools[0])
	}
}

func TestPull_UnknownVersionReturns404(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	publishResp := doPublish(t, server, testPublishToken)
	publishResp.Body.Close()

	resp := doPull(t, server, "9.9.9", testAPIKey)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
