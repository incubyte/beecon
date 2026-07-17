// Command registry is Beecon's separate tool-registry service (Phase 5
// registry sub-phase, PD59): a standalone deployable that shares only the
// registrybundle wire format with the installation binary (cmd/beecon) — it
// depends on no domain module, has no database of its own, and stores
// published bundles on disk behind the Store port (PD60). Installations
// pull from it over its own authenticated HTTP API; it never reaches into
// an installation (H6).
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/registryservice"
	"beecon/internal/registryservice/driven/diskstore"
	"beecon/internal/registryservice/driving/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := serve(logger); err != nil {
		logger.Error("registry exited with error", "err", err)
		os.Exit(1)
	}
}

func serve(logger *slog.Logger) error {
	cfg, err := registryservice.LoadConfig()
	if err != nil {
		return fmt.Errorf("config load failed: %w", err)
	}

	store, err := diskstore.NewStore(cfg.StorageDir)
	if err != nil {
		return fmt.Errorf("open bundle storage: %w", err)
	}

	facade := registryservice.NewFacade(store, idgen.Prefixed("tool_"), func() time.Time { return time.Now().UTC() })
	handler := httpapi.NewHandler(facade, httpx.NewErrorRenderer(logger))
	router := buildRouter(handler, cfg)

	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("registry listening", "addr", addr)
	return srv.ListenAndServe()
}

// buildRouter mounts the registry's own routes: publish is guarded by the
// publish token; pull and list-versions (Slice 3's review-before-adopting
// API) are guarded by the installation-facing API key — two distinct
// credentials, since publishing and pulling are different trust boundaries
// (PD60/PD63 vs PD67).
func buildRouter(handler *httpapi.Handler, cfg *registryservice.Config) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	r.Route("/registry/v1/providers/{providerSlug}/bundles", func(r chi.Router) {
		r.With(httpapi.RequireBearerToken(cfg.PublishToken)).Post("/", handler.Publish)
		r.With(httpapi.RequireBearerToken(cfg.APIKey)).Get("/", handler.ListVersions)
		r.With(httpapi.RequireBearerToken(cfg.APIKey)).Get("/{version}", handler.Pull)
	})

	return r
}
