package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// CreateUser handles POST /api/v1/users (org-scoped, PD2): the organization
// is read only from the request context, injected by OrgAuth — never from
// the path, body, or query.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req createUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, organizations.ErrInvalidName("name", "request body must be valid JSON"))
		return
	}
	user, err := h.facade.CreateUser(r.Context(), org, req.Name, req.ExternalID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toUserDTO(user))
}

// GetUser handles GET /api/v1/users/{userId} (org-scoped): a user belonging
// to another organization surfaces identically to an unknown id (PD5).
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	userID := organizations.UserID(chi.URLParam(r, "userId"))
	user, err := h.facade.GetUser(r.Context(), org, userID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toUserDTO(user))
}
