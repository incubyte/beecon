package httpapi

import (
	"net/http"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// ExportConfig handles GET /api/v1/organizations/{orgId}/config/export
// (Slice 9, PD46): mounted under the Admin UI's org-scoped console mount,
// org comes from context like every other console-reused handler. The
// response never includes an API-key/webhook secret, connection,
// credential, user token, or provider definition — there is no field on
// ConfigDocument for any of them.
func (h *Handler) ExportConfig(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	document, err := h.facade.ExportConfig(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toConfigDocumentDTO(document))
}

// ImportConfig handles POST /api/v1/organizations/{orgId}/config/import
// (Slice 9, PD46): dryRun defaults to true — a missing, empty, or
// unrecognized value is treated as a dry-run rather than rejected, so an
// operator who forgets the query param never accidentally writes anything;
// only the literal "false" turns it off. mode defaults to merge at the
// facade layer when the query param is absent.
func (h *Handler) ImportConfig(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req configDocumentDTO
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrValidation("body", "request body must be valid JSON"))
		return
	}
	dryRun := parseDryRun(r.URL.Query().Get("dryRun"))
	mode := organizations.ImportMode(r.URL.Query().Get("mode"))

	result, err := h.facade.ImportConfig(r.Context(), org, req.toDomain(), organizations.ImportOptions{DryRun: dryRun, Mode: mode})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	if dryRun {
		httpx.WriteJSON(w, http.StatusOK, toImportPlanDTO(result))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toImportApplyDTO(result))
}

// parseDryRun defaults to true (PD46: "dryRun defaults to true — a
// missing/unspecified dryRun param IS a dry-run"); only the literal string
// "false" turns it off — anything else (absent, malformed, "true") stays a
// dry-run, the safe default a credential-adjacent endpoint like this one
// should fail toward.
func parseDryRun(raw string) bool {
	return raw != "false"
}
