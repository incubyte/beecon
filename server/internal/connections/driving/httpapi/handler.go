// Package httpapi is the connections module's driving adapter: thin handlers
// that decode requests, call the facade, and render the shared JSON / PD5
// error envelopes. Mounted behind OrgAuth (org-scoped operations on that
// organization's own connections) or, for the browser-facing subset (PD20),
// the dual-auth OrgOrUser combinator — Initiate, Get, List, and Reconnect all
// additionally enforce that a user-token caller only ever sees or acts on
// its own connections.
package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// Handler serves the connections module's operations. It depends only on the
// connections facade and the shared error renderer.
type Handler struct {
	facade *connections.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *connections.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// Initiate handles POST /api/v1/connections/initiate: the organization is
// read only from the request context, injected by OrgAuth or OrgOrUser —
// never from the path, body, or query. When a user token authenticated the
// request (PD20), the userId comes from the token and any userId in the
// request body is ignored.
func (h *Handler) Initiate(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	var req initiateConnectionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, connections.ErrValidation("userId", "request body must be valid JSON"))
		return
	}
	userID := organizations.UserID(req.UserID)
	if tokenUserID, ok := organizations.UserIDFromContext(r.Context()); ok {
		userID = tokenUserID
	}
	initiated, err := h.facade.Initiate(
		r.Context(),
		org,
		userID,
		catalog.IntegrationID(req.IntegrationID),
		req.RedirectURI,
	)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toInitiatedConnectionDTO(initiated))
}

// Get handles GET /api/v1/connections/{connectionId}: a connection belonging
// to another organization surfaces identically to an unknown id (PD5). When
// a user token authenticated the request (PD20), a connection belonging to
// a different user surfaces the same way — not-found, never a distinct
// "forbidden" that would leak the id's existence.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	connectionID := connections.ConnectionID(chi.URLParam(r, "connectionId"))
	connection, err := h.facade.Get(r.Context(), org, connectionID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	if !callerOwnsConnection(r.Context(), connection) {
		h.errors.WriteError(w, r, connections.ErrNotFound())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toConnectionDTO(connection))
}

// List handles GET /api/v1/connections (Slice 4, AC1): filtered by userId,
// cursor-paginated, scoped to the caller's own organization. When a user
// token authenticated the request (PD20), the filter is forced to that
// token's own user regardless of any userId query parameter.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	query := r.URL.Query()
	limit, err := parseIntQueryParam(query.Get("limit"))
	if err != nil {
		h.errors.WriteError(w, r, connections.ErrValidation("limit", "must be a positive integer"))
		return
	}
	userIDFilter := query.Get("userId")
	if tokenUserID, ok := organizations.UserIDFromContext(r.Context()); ok {
		userIDFilter = string(tokenUserID)
	}
	result, err := h.facade.List(r.Context(), org, connections.ListParams{
		UserID: userIDFilter,
		Cursor: query.Get("cursor"),
		Limit:  limit,
	})
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toConnectionsPageDTO(result))
}

// Disable handles POST /api/v1/connections/{connectionId}/disable (Slice 4,
// AC2): a connection belonging to another organization is not-found (AC11).
func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	connectionID := connections.ConnectionID(chi.URLParam(r, "connectionId"))
	connection, err := h.facade.Disable(r.Context(), org, connectionID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toConnectionStatusDTO(connection))
}

// Delete handles DELETE /api/v1/connections/{connectionId} (Slice 4, AC3): a
// connection belonging to another organization is not-found (AC11).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	connectionID := connections.ConnectionID(chi.URLParam(r, "connectionId"))
	if err := h.facade.Delete(r.Context(), org, connectionID); err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Reconnect handles POST /api/v1/connections/{connectionId}/reconnect
// (Slice 4, AC4): a connection belonging to another organization is
// not-found (AC11). When a user token authenticated the request (PD20), a
// connection belonging to a different user is not-found the same way — the
// browser surface may only reconnect its own connections.
func (h *Handler) Reconnect(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	connectionID := connections.ConnectionID(chi.URLParam(r, "connectionId"))
	if _, ok := organizations.UserIDFromContext(r.Context()); ok {
		connection, err := h.facade.Get(r.Context(), org, connectionID)
		if err != nil {
			h.errors.WriteError(w, r, err)
			return
		}
		if !callerOwnsConnection(r.Context(), connection) {
			h.errors.WriteError(w, r, connections.ErrNotFound())
			return
		}
	}
	var req reconnectRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		h.errors.WriteError(w, r, connections.ErrValidation("redirectUri", "request body must be valid JSON"))
		return
	}
	initiated, err := h.facade.Reconnect(r.Context(), org, connectionID, req.RedirectURI)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toInitiatedConnectionDTO(initiated))
}

// callerOwnsConnection reports whether the caller may see connection: always
// true for an org-key request (no UserID in context); for a user-token
// request (PD20), only when connection belongs to that same user.
func callerOwnsConnection(ctx context.Context, connection connections.Connection) bool {
	tokenUserID, ok := organizations.UserIDFromContext(ctx)
	if !ok {
		return true
	}
	return connection.UserID == tokenUserID
}

func parseIntQueryParam(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
