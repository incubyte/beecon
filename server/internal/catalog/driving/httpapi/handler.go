// Package httpapi is the catalog module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Create is mounted behind AdminAuth (installation-level);
// List is mounted behind OrgAuth (PD7: every organization sees the same
// installation-wide list).
package httpapi

import (
	"net/http"

	"beecon/internal/httpx"
	"beecon/internal/organizations"

	"beecon/internal/catalog"
)

// Handler serves the catalog module's integration operations. It depends
// only on the catalog facade and the shared error renderer.
type Handler struct {
	facade *catalog.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *catalog.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Create handles POST /api/v1/integrations (admin).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createIntegrationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, catalog.ErrValidation("providerSlug", "request body must be valid JSON"))
		return
	}
	summary, err := h.facade.CreateIntegration(r.Context(), req.ProviderSlug, req.ClientID, req.ClientSecret)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toIntegrationSummaryDTO(summary))
}

// List handles GET /api/v1/integrations (org-scoped route, installation-wide
// result per PD7): the caller must be authenticated as an organization, but
// which organization is irrelevant to what the endpoint returns.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := organizations.OrgIDFromContext(r.Context()); !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	summaries, err := h.facade.ListIntegrations(r.Context())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toIntegrationSummaryDTOs(summaries))
}
