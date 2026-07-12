// Package httpapi is the execution module's driving adapter: a thin handler
// that decodes the request, calls the facade, and renders the shared JSON /
// PD5 error envelopes. Mounted behind the OrgAuth middleware.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/connections"
	"beecon/internal/execution"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves POST /api/v1/tools/{slug}/execute. It depends only on the
// execution facade and the shared error renderer.
type Handler struct {
	facade *execution.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *execution.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Execute handles POST /api/v1/tools/{slug}/execute: an unknown tool slug or
// a connectionId outside the caller's organization or userID renders as a
// PD5 HTTP error (PD6); everything else is a tool-level failure inside a
// 200 Result.
func (h *Handler) Execute(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	slug := chi.URLParam(r, "slug")

	var req executeToolRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, execution.ErrValidation("arguments", "request body must be valid JSON"))
		return
	}

	result, err := h.facade.Execute(
		r.Context(),
		org,
		organizations.UserID(req.UserID),
		connections.ConnectionID(req.ConnectionID),
		slug,
		req.Arguments,
	)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toExecutionResultDTO(result))
}
