// Package httpapi is the organizations module's driving adapter: thin
// handlers that decode requests, call the facade, and render the shared JSON
// / PD5 error envelopes. Mounted behind the AdminAuth middleware — every
// route here is an installation-level operation.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the organizations operations. It depends only on the
// organizations facade and the shared error renderer.
type Handler struct {
	facade *organizations.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *organizations.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createOrganizationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrInvalidName("name", "request body must be valid JSON"))
		return
	}
	org, err := h.facade.Create(r.Context(), req.Name)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toOrganizationDTO(org))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	orgID := organizations.OrgID(chi.URLParam(r, "orgId"))
	org, err := h.facade.Get(r.Context(), orgID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOrganizationDTO(org))
}
