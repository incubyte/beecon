// Package httpapi (in-package test) exercises the handlers through an actual
// chi router mounted with the same route shape app/router.go uses
// ("/{orgId}/api-keys", "/{orgId}/api-keys/{keyId}"), backed by the
// driven/memory facade, so chi.URLParam resolution and the PD5 error
// envelope are exercised exactly as production wires them.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/httpx"
)

func newTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/{orgId}/api-keys", h.Issue)
	r.Get("/{orgId}/api-keys", h.List)
	r.Delete("/{orgId}/api-keys/{keyId}", h.Revoke)
	return r
}

func doRequest(r chi.Router, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type wireErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, w *httptest.ResponseRecorder) wireErrorEnvelope {
	t.Helper()
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("response body is not the PD5 error envelope: %v\nbody: %s", err, w.Body.String())
	}
	return env
}

func TestIssue_Returns201WithTheFullSecretPresentExactlyHere(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodPost, "/org_1/api-keys")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto issuedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "key_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "key_")
	}
	if !strings.HasPrefix(dto.Key, access.SecretPrefix) {
		t.Errorf("key = %q, want it to start with %q", dto.Key, access.SecretPrefix)
	}
	if len(dto.Prefix) != access.LookupPrefixLength {
		t.Errorf("prefix = %q (len %d), want length %d", dto.Prefix, len(dto.Prefix), access.LookupPrefixLength)
	}
	if dto.CreatedAt == "" {
		t.Error("createdAt is empty, want a non-empty timestamp")
	}
}

func TestList_Returns200WithNoSecretFieldAnywhereInTheJSON(t *testing.T) {
	r := newTestRouter()
	doRequest(r, http.MethodPost, "/org_1/api-keys")
	doRequest(r, http.MethodPost, "/org_1/api-keys")

	w := doRequest(r, http.MethodGet, "/org_1/api-keys")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if _, present := entry["key"]; present {
			t.Fatalf("list entry %+v carries a %q field — the full secret must never appear in List", entry, "key")
		}
		for _, wantField := range []string{"id", "prefix", "createdAt"} {
			if _, present := entry[wantField]; !present {
				t.Errorf("list entry %+v is missing expected field %q", entry, wantField)
			}
		}
	}
}

func TestRevoke_Returns204NoContent(t *testing.T) {
	r := newTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequest(r, http.MethodDelete, "/org_1/api-keys/"+issued.ID)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
	}
}

func TestRevoke_Returns404ForAnUnknownKeyIDWithThePD5NotFoundEnvelope(t *testing.T) {
	r := newTestRouter()

	w := doRequest(r, http.MethodDelete, "/org_1/api-keys/key_missing")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
	}
}
