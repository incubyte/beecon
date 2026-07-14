// Package httpapi is the triggers module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Every route is mounted behind OrgAuth (all org auth, per
// the API Shape) — trigger instances have no browser-facing subset today.
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"beecon/internal/connections"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
)

// Handler serves the triggers module's operations. It depends only on the
// triggers facade and the shared error renderer.
type Handler struct {
	facade *triggers.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *triggers.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Create handles POST /api/v1/trigger-instances: binds a Connection to a
// trigger definition (PD33). The organization is read only from the request
// context, injected by OrgAuth.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req createTriggerInstanceRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, triggers.ErrValidation("triggerSlug", "request body must be valid JSON"))
		return
	}
	instance, err := h.facade.Create(r.Context(), org, triggers.CreateParams{
		ConnectionID: connections.ConnectionID(req.ConnectionID),
		TriggerSlug:  req.TriggerSlug,
		Config:       req.Config,
	})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toCreatedTriggerInstanceDTO(instance))
}

// List handles GET /api/v1/trigger-instances: filtered by connectionId or
// userId, cursor-paginated, scoped to the caller's own organization (PD33).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	query := r.URL.Query()
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, triggers.ErrValidation("limit", "must be a positive integer"))
		return
	}
	result, err := h.facade.List(r.Context(), org, triggers.ListParams{
		ConnectionID: query.Get("connectionId"),
		UserID:       query.Get("userId"),
		Cursor:       query.Get("cursor"),
		Limit:        limit,
	})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerInstancesPageDTO(result))
}

// Get handles GET /api/v1/trigger-instances/{trgId}: an instance belonging
// to another organization surfaces identically to an unknown id (PD33).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	instance, err := h.facade.Get(r.Context(), org, instanceIDFromRequest(r))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerInstanceDTO(instance))
}

// Disable handles POST /api/v1/trigger-instances/{trgId}/disable (PD33): an
// instance belonging to another organization is not-found.
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	instance, err := h.facade.Disable(r.Context(), org, instanceIDFromRequest(r))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerInstanceStatusDTO(instance))
}

// Enable handles POST /api/v1/trigger-instances/{trgId}/enable (PD33): an
// instance belonging to another organization is not-found.
func (h *Handler) Enable(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	instance, err := h.facade.Enable(r.Context(), org, instanceIDFromRequest(r))
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toTriggerInstanceStatusDTO(instance))
}

// Delete handles DELETE /api/v1/trigger-instances/{trgId} (PD33): an
// instance belonging to another organization is not-found.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	if err := h.facade.Delete(r.Context(), org, instanceIDFromRequest(r)); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func instanceIDFromRequest(r *http.Request) triggers.TriggerInstanceID {
	return triggers.TriggerInstanceID(chi.URLParam(r, "trgId"))
}

func parseIntQueryParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
