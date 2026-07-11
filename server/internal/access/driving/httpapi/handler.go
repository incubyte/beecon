// Package httpapi is the access module's driving adapter: thin handlers that
// decode requests, call the facade, and render the shared JSON / PD5 error
// envelopes. Mounted behind the AdminAuth middleware — every route here is
// an installation-level operation on an organization's keys.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the access module's key operations. It depends only on the
// access facade and the shared error renderer.
type Handler struct {
	facade *access.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *access.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Issue handles POST /api/v1/organizations/{orgId}/api-keys: the full
// secret is returned exactly once, at creation.
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	org := organizations.OrgID(chi.URLParam(r, "orgId"))
	issued, err := h.facade.Issue(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toIssuedKeyDTO(issued))
}

// List handles GET /api/v1/organizations/{orgId}/api-keys.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	org := organizations.OrgID(chi.URLParam(r, "orgId"))
	keys, err := h.facade.List(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toKeyDTOs(keys))
}

// Revoke handles DELETE /api/v1/organizations/{orgId}/api-keys/{keyId}.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	org := organizations.OrgID(chi.URLParam(r, "orgId"))
	keyID := access.KeyID(chi.URLParam(r, "keyId"))
	if err := h.facade.Revoke(r.Context(), org, keyID); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
