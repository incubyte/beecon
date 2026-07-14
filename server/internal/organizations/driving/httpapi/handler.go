// Package httpapi is the organizations module's driving adapter: thin
// handlers that decode requests, call the facade, and render the shared JSON
// / PD5 error envelopes. Mounted behind the AdminAuth middleware — every
// route here is an installation-level operation.
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the organizations operations. It depends only on the
// organizations facade and the shared error renderer.
type Handler struct {
	facade *organizations.Facade
	errors *httpx.ErrorRenderer

	// installationDefaultRetentionDays is GetRetention/UpdateRetention's own
	// display value (Slice 7, PD44): the facade itself carries no opinion
	// about BEECON_RETENTION_DAYS (it stays config-free), so this handler —
	// which app/wiring.go already hands the configured value to, mirroring
	// executionhttp.NewFilesHandler's own baseURL parameter — echoes it in
	// retentionDTO so the console can render "inherit default (N)" without
	// hardcoding N. Left at its zero value, defaultInstallationRetentionDays
	// is used instead — harmless for every existing test constructor that
	// never calls WithInstallationDefaultRetentionDays and doesn't care what
	// number appears in that one display field.
	installationDefaultRetentionDays int
}

func NewHandler(facade *organizations.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// WithInstallationDefaultRetentionDays sets the value GetRetention/
// UpdateRetention echo as retentionDTO.InstallationDefaultDays (Slice 7).
// Production wiring always calls this with the configured
// BEECON_RETENTION_DAYS; test constructors that never call it fall back to
// defaultInstallationRetentionDays.
func (h *Handler) WithInstallationDefaultRetentionDays(days int) *Handler {
	h.installationDefaultRetentionDays = days
	return h
}

func (h *Handler) installationDefaultRetentionDaysOrFallback() int {
	if h.installationDefaultRetentionDays <= 0 {
		return defaultInstallationRetentionDays
	}
	return h.installationDefaultRetentionDays
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

// List handles GET /api/v1/organizations (Slice 1, PD40): every
// organization in the installation, cursor-paginated, newest first — an
// operator-only, installation-wide view guarded by AdminAuth.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, organizations.ErrValidation("limit", "must be a positive integer"))
		return
	}
	result, err := h.facade.ListAll(r.Context(), organizations.ListAllParams{
		Cursor: query.Get("cursor"),
		Limit:  limit,
	})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOrganizationsPageDTO(result))
}

func parseIntQueryParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}

// UpdateAllowedRedirectURIs handles PATCH /api/v1/organizations/{orgId}
// (PD4): it replaces the organization's redirect-uri allow-list.
func (h *Handler) UpdateAllowedRedirectURIs(w http.ResponseWriter, r *http.Request) {
	orgID := organizations.OrgID(chi.URLParam(r, "orgId"))
	var req updateAllowedRedirectURIsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrValidation("allowedRedirectUris", "request body must be valid JSON"))
		return
	}
	org, err := h.facade.SetAllowedRedirectURIs(r.Context(), orgID, req.AllowedRedirectUris)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toOrganizationDTO(org))
}

// GetGovernance handles GET /api/v1/organizations/{orgId}/governance (Slice
// 5): mounted under the Admin UI's org-scoped console mount
// (AdminOrgScope/InjectOrgFromPath), so org comes from context like every
// other console-reused handler. Synthesizes the continuity-preserving
// default (PD42) for an org that has never been configured.
func (h *Handler) GetGovernance(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	governance, err := h.facade.GetGovernance(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toGovernanceDTO(governance))
}

// UpdateGovernance handles PUT /api/v1/organizations/{orgId}/governance
// (Slice 5): replaces the org's entire governance record — allow-list,
// hidden set, and onboarding featured/cap.
func (h *Handler) UpdateGovernance(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req governanceRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrValidation("governance", "request body must be valid JSON"))
		return
	}
	governance, err := h.facade.SetGovernance(r.Context(), org, req.toUpdate())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toGovernanceDTO(governance))
}

// GetRetention handles GET /api/v1/organizations/{orgId}/retention (Slice
// 7, PD44): mounted under the same org-scoped console mount as
// GetGovernance, org comes from context the same way.
func (h *Handler) GetRetention(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	view, err := h.facade.GetRetention(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRetentionDTO(view, h.installationDefaultRetentionDaysOrFallback()))
}

// UpdateRetention handles PUT /api/v1/organizations/{orgId}/retention
// (Slice 7, PD44): replaces the org's own log/event retention windows —
// leaving its governance (allow-list/hidden/onboarding) untouched.
func (h *Handler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req retentionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrValidation("retention", "request body must be valid JSON"))
		return
	}
	view, err := h.facade.SetRetention(r.Context(), org, req.toUpdate())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRetentionDTO(view, h.installationDefaultRetentionDaysOrFallback()))
}
