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

// List handles GET /api/v1/integrations (org-scoped route): returns org's
// visible integrations (Slice 5, PD42 — filtered by allow-list/hidden; an
// org with no governance configured sees the full installation catalog,
// exactly PD7's original behavior). ?featured=true (PD43) returns the
// org's ordered onboarding subset instead.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var summaries []catalog.IntegrationSummary
	var err error
	if parseBoolQueryParam(r.URL.Query().Get("featured")) {
		summaries, err = h.facade.ListFeaturedIntegrations(r.Context(), org)
	} else {
		summaries, err = h.facade.ListIntegrations(r.Context(), org)
	}
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toIntegrationSummaryDTOs(summaries))
}

// ListWithVisibility handles GET
// /api/v1/organizations/{orgId}/governance/catalog (Slice 5, AC1): mounted
// under the Admin UI's org-scoped console mount, so org comes from context
// like every other console-reused handler. Returns every installation
// integration, unfiltered, each annotated with its effective visibility for
// org — the operator's governance view, distinct from List's already-
// filtered, org-facing result.
func (h *Handler) ListWithVisibility(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	items, err := h.facade.ListIntegrationsWithVisibility(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toIntegrationVisibilityDTOs(items))
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

// ListTriggerDefinitions handles GET /api/v1/trigger-definitions (org **or**
// user token, per the API Shape): filters by integrationId or providerSlug,
// cursor-paginated.
func (h *Handler) ListTriggerDefinitions(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}

	query := r.URL.Query()
	filter := catalog.TriggerDefinitionFilter{
		IntegrationID: catalog.IntegrationID(query.Get("integrationId")),
		ProviderSlug:  query.Get("providerSlug"),
	}
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, catalog.ErrValidation("limit", "must be a positive integer"))
		return
	}

	page, err := h.facade.ListTriggerDefinitions(r.Context(), org, filter, query.Get("cursor"), limit)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerDefinitionsPageDTO(page))
}

// GetTriggerDefinition handles GET /api/v1/trigger-definitions/{slug}
// (org-scoped route): the same detail ListTriggerDefinitions carries for one
// trigger, addressed by slug (PD14).
func (h *Handler) GetTriggerDefinition(w http.ResponseWriter, r *http.Request) {
	if _, ok := organizations.OrgIDFromContext(r.Context()); !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	trigger, err := h.facade.TriggerDefinitionDetail(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerDefinitionSummaryDTO(trigger))
}

// ListProviderDefinitions handles GET /api/v1/provider-definitions
// (admin-guarded, installation-wide route; PD40, Slice 6, AC1): every
// provider definition this installation has loaded, cursor-paginated,
// unfiltered by any organization's governance (AC7) — the operator's real
// installed estate, not the org-facing filtered catalog ListIntegrations
// returns.
func (h *Handler) ListProviderDefinitions(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, catalog.ErrValidation("limit", "must be a positive integer"))
		return
	}
	page, err := h.facade.ListProviderDefinitions(r.Context(), query.Get("cursor"), limit)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toProviderDefinitionsPageDTO(page))
}

// GetProviderDefinition handles GET /api/v1/provider-definitions/{slug}
// (admin-guarded, installation-wide route; PD40, Slice 6, AC2): the
// definition's full versioned bundle. An unknown slug is not-found.
func (h *Handler) GetProviderDefinition(w http.ResponseWriter, r *http.Request) {
	detail, err := h.facade.ProviderDefinitionDetail(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toProviderDefinitionDetailDTO(detail))
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
