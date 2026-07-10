package httpx_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/httpx"
)

// wireErrorEnvelope pins the PD5 wire shape literally:
// {"error": {"code": "...", "message": "...", "details": {...}}}.
type wireErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

func TestWriteJSON_WritesThePayloadWithTheGivenStatus(t *testing.T) {
	w := httptest.NewRecorder()

	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": "org_1"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["id"] != "org_1" {
		t.Errorf("body[id] = %q, want %q", body["id"], "org_1")
	}
}

func TestWriteDomainError_RendersThePD5EnvelopeShapeLiterally(t *testing.T) {
	w := httptest.NewRecorder()
	err := httpx.New(http.StatusUnprocessableEntity, "validation_failed", "validation failed").
		WithDetails(map[string]any{"field": "name", "issue": "must not be empty"})

	httpx.WriteDomainError(w, err)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}
	wantBody := `{"error":{"code":"validation_failed","message":"validation failed","details":{"field":"name","issue":"must not be empty"}}}` + "\n"
	if got := w.Body.String(); got != wantBody {
		t.Errorf("body = %s, want %s", got, wantBody)
	}
}

func TestWriteDomainError_MapsUnauthorizedTo401(t *testing.T) {
	w := httptest.NewRecorder()

	httpx.WriteDomainError(w, httpx.Unauthorized("missing or invalid admin key"))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestWriteDomainError_NilErrorRendersA500InternalError(t *testing.T) {
	w := httptest.NewRecorder()

	httpx.WriteDomainError(w, nil)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "internal_error")
	}
}

func TestErrorRenderer_WriteError_RendersDomainErrorsAsTheirOwnStatus(t *testing.T) {
	renderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/organizations/org_missing", nil)

	renderer.WriteError(w, r, httpx.New(http.StatusNotFound, "not_found", "organization not found"))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}

func TestErrorRenderer_WriteError_RendersUnknownErrorsAs500InternalError(t *testing.T) {
	renderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/organizations", nil)

	renderer.WriteError(w, r, errors.New("boom: unexpected database failure"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "internal_error" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "internal_error")
	}
	// The raw error text must never leak to the client.
	if got := w.Body.String(); strings.Contains(got, "boom") {
		t.Errorf("body must not leak the underlying error text, got %s", got)
	}
}
