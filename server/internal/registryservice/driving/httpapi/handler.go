// Package httpapi is the registry service's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared PD5 error
// envelope (httpx is shared infrastructure, importable by any module — this
// registry binary is no exception even though it depends on no domain
// module).
package httpapi

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/registryservice"
)

// Handler serves the registry service's publish and pull operations.
type Handler struct {
	facade *registryservice.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *registryservice.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Publish handles POST /registry/v1/providers/{providerSlug}/bundles
// (RequireBearerToken(publishToken)-guarded): strictly parses the bundle
// body (Slice 2's strict-parse gate — an unknown/misspelled field is
// rejected rather than silently dropped, PD63), mints tool_ ids, resolves
// and enforces the version, and returns the assigned version alongside
// every tool's id and what changed relative to the previous version.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	providerSlug := chi.URLParam(r, "providerSlug")

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		h.errors.WriteError(w, r, registryservice.ErrValidation("body", "failed to read request body"))
		return
	}
	bundle, err := registryservice.ParseBundleStrict(raw)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}

	result, err := h.facade.Publish(r.Context(), providerSlug, bundle)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toPublishResultDTO(result))
}

// Pull handles GET /registry/v1/providers/{providerSlug}/bundles/{version}
// (RequireBearerToken(apiKey)-guarded): returns the full bundle, including
// every tool's tool_ id and its input/output schemas (Slice 1's pull API).
func (h *Handler) Pull(w http.ResponseWriter, r *http.Request) {
	providerSlug := chi.URLParam(r, "providerSlug")
	version := chi.URLParam(r, "version")

	bundle, err := h.facade.Pull(r.Context(), providerSlug, version)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bundle)
}

// ListVersions handles GET /registry/v1/providers/{providerSlug}/bundles
// (RequireBearerToken(apiKey)-guarded, the same pull trust boundary as Pull
// — PD67): every version this provider has published, its content hash, and
// when it was published (Slice 3) — an installation operator reviews this
// before pulling/activating any particular version.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	providerSlug := chi.URLParam(r, "providerSlug")

	versions, err := h.facade.ListVersions(r.Context(), providerSlug)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toBundleVersionsDTO(versions))
}
