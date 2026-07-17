package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	"beecon/internal/httpx"
)

// RegistryHandler serves the installation-side registry-sync operations
// (Phase 5 registry sub-phase, PD64). It is mounted behind ConsoleAuth
// (operator session + CSRF) — activation is a mutating console operation,
// per the spec's binding auth decision, never the demoted break-glass
// admin key alone post-bootstrap.
type RegistryHandler struct {
	facade *catalog.Facade
	errors *httpx.ErrorRenderer
}

func NewRegistryHandler(facade *catalog.Facade, errors *httpx.ErrorRenderer) *RegistryHandler {
	return &RegistryHandler{facade: facade, errors: errors}
}

// Activate handles
// POST /api/v1/registry/providers/{slug}/activate {version} (Slice 1): pulls
// providerSlug's bundle at the requested version from the registry and
// activates it — after which this installation's catalog serves that
// version's tools and triggers without a redeploy.
func (h *RegistryHandler) Activate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	var req activateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, catalog.ErrValidation("version", "request body must be valid JSON"))
		return
	}
	if req.Version == "" {
		h.errors.WriteError(w, r, catalog.ErrValidation("version", "must not be empty"))
		return
	}

	activated, err := h.facade.Activate(r.Context(), slug, req.Version)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toActivatedVersionDTO(activated))
}

// ListVersions handles GET /api/v1/registry/providers/{slug}/versions
// (Slice 3): every version the registry offers for slug, each marked
// active/not against this installation's currently activated version — an
// operator reviews this before requesting a diff or activating anything.
func (h *RegistryHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	view, err := h.facade.ListRegistryVersions(r.Context(), slug)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRegistryVersionsDTO(view))
}

// Diff handles GET /api/v1/registry/providers/{slug}/diff?to={version}
// (Slice 3): pulls the target version from the registry and reports what it
// would add, change, or remove relative to the version currently active in
// this installation — a pure read, activating nothing.
func (h *RegistryHandler) Diff(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	to := r.URL.Query().Get("to")
	if to == "" {
		h.errors.WriteError(w, r, catalog.ErrValidation("to", "must not be empty"))
		return
	}

	diff, err := h.facade.DiffRegistryVersion(r.Context(), slug, to)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRegistryDiffDTO(diff))
}
