// Package httpapi is the delivery module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Every route is mounted behind OrgAuth (API Shape: the
// webhook channel is org-key-only, no browser-facing subset).
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"beecon/internal/delivery"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the delivery module's operations. It depends only on the
// delivery facade and the shared error renderer.
type Handler struct {
	facade *delivery.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *delivery.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// SetEndpoint handles PUT /api/v1/webhook-endpoint (PD31).
func (h *Handler) SetEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	var req setWebhookEndpointRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("url", "request body must be valid JSON"))
		return
	}
	result, err := h.facade.SetEndpoint(r.Context(), org, req.URL)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWebhookEndpointCreatedDTO(result))
}

// GetEndpoint handles GET /api/v1/webhook-endpoint (PD31): never the full
// secret.
func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	view, err := h.facade.GetEndpoint(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWebhookEndpointDTO(view))
}

// RotateSecret handles POST /api/v1/webhook-endpoint/rotate-secret (PD31).
func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	var req rotateSecretRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("overlapHours", "request body must be valid JSON"))
		return
	}
	result, err := h.facade.RotateSecret(r.Context(), org, req.OverlapHours)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRotatedSecretDTO(result))
}

// SendTest handles POST /api/v1/webhook-endpoint/test: 202, the delivery
// itself happens asynchronously via the dispatcher loop.
func (h *Handler) SendTest(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	if _, err := h.facade.SendTest(r.Context(), org); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ListEvents handles GET /api/v1/events: filtered by type and/or
// deliveryStatus, cursor-paginated.
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("limit", "must be a positive integer"))
		return
	}
	result, err := h.facade.ListEvents(r.Context(), org, delivery.ListEventsParams{
		Type:           query.Get("type"),
		DeliveryStatus: query.Get("deliveryStatus"),
		Cursor:         query.Get("cursor"),
		Limit:          limit,
	})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEventsPageDTO(result))
}

// Redeliver handles POST /api/v1/events/{evtId}/redeliver: 202, re-queues
// the event for another attempt.
func (h *Handler) Redeliver(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EventID(chi.URLParam(r, "evtId"))
	if _, err := h.facade.Redeliver(r.Context(), org, id); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ListEndpoints handles GET /api/v1/webhook-endpoints (Slice 8, PD45): the
// multi-endpoint CRUD surface's own read, never a secret.
func (h *Handler) ListEndpoints(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	items, err := h.facade.ListEndpoints(r.Context(), org)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toEndpointListDTO(items))
}

// CreateEndpoint handles POST /api/v1/webhook-endpoints (Slice 8, AC1):
// 201, the secret shown exactly once. Rejected with a validation error
// naming the cap once the org already holds BEECON_WEBHOOK_ENDPOINT_CAP
// endpoints (AC2).
func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	var req createEndpointRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("url", "request body must be valid JSON"))
		return
	}
	result, err := h.facade.CreateEndpoint(r.Context(), org, req.URL, req.eventTypes())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toCreateEndpointDTO(result))
}

// UpdateEndpoint handles PUT /api/v1/webhook-endpoints/{wepId} (Slice 8):
// a whole-object update of url and the event-type filter.
func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EndpointID(chi.URLParam(r, "wepId"))
	var req updateEndpointRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("url", "request body must be valid JSON"))
		return
	}
	result, err := h.facade.UpdateEndpoint(r.Context(), org, id, req.URL, req.eventTypes())
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUpdateEndpointDTO(result))
}

// DeleteEndpoint handles DELETE /api/v1/webhook-endpoints/{wepId} (Slice 8,
// AC8): 204.
func (h *Handler) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EndpointID(chi.URLParam(r, "wepId"))
	if err := h.facade.DeleteEndpoint(r.Context(), org, id); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RotateEndpointSecret handles POST
// /api/v1/webhook-endpoints/{wepId}/rotate-secret (Slice 8, AC8): the new
// secret shown exactly once.
func (h *Handler) RotateEndpointSecret(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EndpointID(chi.URLParam(r, "wepId"))
	var req rotateSecretRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, delivery.ErrValidation("overlapHours", "request body must be valid JSON"))
		return
	}
	result, err := h.facade.RotateEndpointSecret(r.Context(), org, id, req.OverlapHours)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toRotatedSecretDTO(result))
}

// EnableEndpoint handles POST /api/v1/webhook-endpoints/{wepId}/enable
// (Slice 8, AC6): resumes fan-out and resets the consecutive-failure
// counter, including for an endpoint auto-disable bookkeeping quarantined.
func (h *Handler) EnableEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EndpointID(chi.URLParam(r, "wepId"))
	result, err := h.facade.EnableEndpoint(r.Context(), org, id)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUpdateEndpointDTO(result))
}

// DisableEndpoint handles POST /api/v1/webhook-endpoints/{wepId}/disable
// (Slice 8): an operator-initiated disable, distinct from the
// DISABLED_AUTO auto-disable bookkeeping sets on its own.
func (h *Handler) DisableEndpoint(w http.ResponseWriter, r *http.Request) {
	org, ok := h.orgFromRequest(w, r)
	if !ok {
		return
	}
	id := delivery.EndpointID(chi.URLParam(r, "wepId"))
	result, err := h.facade.DisableEndpoint(r.Context(), org, id)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUpdateEndpointDTO(result))
}

func (h *Handler) orgFromRequest(w http.ResponseWriter, r *http.Request) (organizations.OrgID, bool) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return "", false
	}
	return org, true
}

func parseIntQueryParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
