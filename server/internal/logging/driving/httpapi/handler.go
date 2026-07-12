// Package httpapi is the logging module's driving adapter: a thin handler
// that decodes query parameters, calls the facade, and renders the shared
// JSON / PD5 error envelopes. Mounted behind the OrgAuth middleware — a
// caller only ever sees its own organization's log entries (AC10).
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/logging"
	"beecon/internal/organizations"
)

// Handler serves the logging module's query operation. It depends only on
// the logging facade and the shared error renderer.
type Handler struct {
	facade *logging.Facade
	errors *httpx.ErrorRenderer
}

func NewHandler(facade *logging.Facade, errors *httpx.ErrorRenderer) *Handler {
	return &Handler{facade: facade, errors: errors}
}

// List handles GET /api/v1/logs: filtered by connectionId, userId, toolSlug,
// and a from/to time range, cursor-paginated newest first, scoped to the
// caller's own organization (AC10).
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	params, err := parseQueryParams(r)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	result, err := h.facade.Query(r.Context(), org, params)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toLogsPageDTO(result))
}

// parseQueryParams reads GET /api/v1/logs's query string into a
// logging.QueryParams, rejecting a malformed from/to/limit value.
func parseQueryParams(r *http.Request) (logging.QueryParams, error) {
	query := r.URL.Query()
	params := logging.QueryParams{
		ConnectionID: query.Get("connectionId"),
		UserID:       query.Get("userId"),
		ToolSlug:     query.Get("toolSlug"),
		Cursor:       query.Get("cursor"),
	}

	from, err := parseTimeParam(query.Get("from"), "from")
	if err != nil {
		return logging.QueryParams{}, err
	}
	params.From = from

	to, err := parseTimeParam(query.Get("to"), "to")
	if err != nil {
		return logging.QueryParams{}, err
	}
	params.To = to

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return logging.QueryParams{}, logging.ErrInvalidLimit()
		}
		params.Limit = limit
	}
	return params, nil
}

func parseTimeParam(raw, field string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, logging.ErrInvalidTimeRange(field)
	}
	return &parsed, nil
}
