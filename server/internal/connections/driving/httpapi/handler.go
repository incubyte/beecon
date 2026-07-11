// Package httpapi is the connections module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Mounted behind the OrgAuth middleware — every route here
// is an org-scoped operation on that organization's own connections.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the connections module's operations. It depends only on the
// connections facade and the shared error renderer.
type Handler struct {
	facade *connections.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *connections.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Initiate handles POST /api/v1/connections/initiate: the organization is
// read only from the request context, injected by OrgAuth — never from the
// path, body, or query.
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req initiateConnectionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, connections.ErrValidation("userId", "request body must be valid JSON"))
		return
	}
	initiated, err := h.facade.Initiate(
		r.Context(),
		org,
		organizations.UserID(req.UserID),
		catalog.IntegrationID(req.IntegrationID),
		req.RedirectURI,
	)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toInitiatedConnectionDTO(initiated))
}

// Get handles GET /api/v1/connections/{connectionId}: a connection belonging
// to another organization surfaces identically to an unknown id (PD5).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	connectionID := connections.ConnectionID(chi.URLParam(r, "connectionId"))
	connection, err := h.facade.Get(r.Context(), org, connectionID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toConnectionDTO(connection))
}
