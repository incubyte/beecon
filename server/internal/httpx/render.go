package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

// errorBody is the PD5 error envelope: {"error": {"code": "...", "message": "..."}}.
type errorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

// WriteJSON writes the payload as the response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// WriteDomainError renders a DomainError as the PD5 envelope. A nil err
// renders a 500 internal_error.
func WriteDomainError(w http.ResponseWriter, err *DomainError) {
	if err == nil {
		writeInternalError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	for key, value := range err.Headers {
		w.Header().Set(key, value)
	}
	w.WriteHeader(err.Status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Code:    err.Code,
		Message: err.Message,
		Details: err.Details,
	}})
}

func writeInternalError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Code:    "internal_error",
		Message: "Internal Server Error",
	}})
}

// ErrorRenderer is the shared error-render seam: it unwraps a *DomainError and
// renders it via WriteDomainError, and logs any other error with request
// context before rendering the 500 internal_error envelope. The logger is
// injected so tests can capture the log line.
type ErrorRenderer struct {
	logger *slog.Logger
}

// NewErrorRenderer returns an ErrorRenderer using the given logger, falling
// back to slog.Default() when logger is nil to avoid a nil-pointer panic.
func NewErrorRenderer(logger *slog.Logger) *ErrorRenderer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErrorRenderer{logger: logger}
}

// WriteError unwraps a *DomainError and renders it via WriteDomainError;
// everything else is logged with request context and rendered as the 500
// internal_error envelope.
func (e *ErrorRenderer) WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var de *DomainError
	if errors.As(err, &de) {
		WriteDomainError(w, de)
		return
	}
	e.logger.Error("unhandled server error",
		"requestId", middleware.GetReqID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"err", err,
	)
	writeInternalError(w)
}
