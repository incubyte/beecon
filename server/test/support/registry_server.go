//go:build integration

// Package support: NewTestRegistryServer boots a real, separately-served
// instance of the registry service (internal/registryservice) behind an
// httptest.Server — mirroring cmd/registry/main.go's own router assembly
// (publish guarded by a publish token, pull guarded by an installation-facing
// API key) so a crucial-path journey can publish a bundle and have the
// installation pull/activate it over a real HTTP round trip, exactly as
// production does (PD59: the registry is a separate deployable, never
// embedded in the installation binary).
package support

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/registryservice"
	registrymemory "beecon/internal/registryservice/driven/memory"
	registryhttpapi "beecon/internal/registryservice/driving/httpapi"
)

// TestRegistryServer is a real registryservice.Facade + HTTP router served by
// an httptest.Server, plus the two credentials it was configured with.
type TestRegistryServer struct {
	*httptest.Server
	PublishToken string
	APIKey       string
}

// NewTestRegistryServer starts a fresh in-memory-backed registry service,
// registered for cleanup with t.
func NewTestRegistryServer(t *testing.T, publishToken, apiKey string) *TestRegistryServer {
	t.Helper()

	store := registrymemory.NewStore()
	facade := registryservice.NewFacade(store, idgen.Prefixed("tool_"), func() time.Time { return time.Now().UTC() })
	handler := registryhttpapi.NewHandler(facade, httpx.NewErrorRenderer(testLogger()))

	r := chi.NewRouter()
	r.Route("/registry/v1/providers/{providerSlug}/bundles", func(r chi.Router) {
		r.With(registryhttpapi.RequireBearerToken(publishToken)).Post("/", handler.Publish)
		r.With(registryhttpapi.RequireBearerToken(apiKey)).Get("/{version}", handler.Pull)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)

	return &TestRegistryServer{Server: server, PublishToken: publishToken, APIKey: apiKey}
}
