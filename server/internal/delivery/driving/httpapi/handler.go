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
