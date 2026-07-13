// Package httpapi is the catalog module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Create is mounted behind AdminAuth (installation-level);
// List is mounted behind OrgAuth (PD7: every organization sees the same
// installation-wide list).
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

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

// ListTools handles GET /api/v1/tools (org-scoped route): filters by
// integrationId or providerSlug, optionally includes deprecated tools,
// cursor-paginated.
func (h *Handler) ListTools(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}

	query := r.URL.Query()
	filter := catalog.ToolFilter{
		IntegrationID:     catalog.IntegrationID(query.Get("integrationId")),
		ProviderSlug:      query.Get("providerSlug"),
		IncludeDeprecated: parseBoolQueryParam(query.Get("includeDeprecated")),
	}
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, catalog.ErrValidation("limit", "must be a positive integer"))
		return
	}

	page, err := h.facade.ListTools(r.Context(), org, filter, query.Get("cursor"), limit)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toToolsPageDTO(page))
}

// GetTool handles GET /api/v1/tools/{slug} (org-scoped route): the same
// detail ListTools carries for one tool, addressed by slug (PD8).
func (h *Handler) GetTool(w http.ResponseWriter, r *http.Request) {
	if _, ok := organizations.OrgIDFromContext(r.Context()); !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	tool, err := h.facade.ToolDetail(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toToolSummaryDTO(tool))
}

// GetExpectedParams handles GET /api/v1/integrations/{intgId}/expected-params
// (org-scoped route; Slice 3's AC2): an unknown integration id is not-found.
func (h *Handler) GetExpectedParams(w http.ResponseWriter, r *http.Request) {
	if _, ok := organizations.OrgIDFromContext(r.Context()); !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	id := catalog.IntegrationID(chi.URLParam(r, "intgId"))
	view, err := h.facade.GetExpectedParams(r.Context(), id)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toExpectedParamsDTO(view))
}

func parseBoolQueryParam(raw string) bool {
	parsed, _ := strconv.ParseBool(raw)
	return parsed
}

func parseIntQueryParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
