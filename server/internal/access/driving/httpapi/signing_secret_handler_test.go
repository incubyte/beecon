// Package httpapi (in-package test; see handler_test.go for the shared
// doRequest/wireErrorEnvelope/decodeError helpers reused here). This file
// exercises the admin signing-secret endpoints (PD20) through a chi router
// mounted with the same route shape app/router.go uses
// ("/{orgId}/signing-secrets"), backed by the driven/memory facade.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	memory "beecon/internal/access/driven/memory"
	"beecon/internal/httpx"
)

func newSigningSecretTestRouter() chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/{orgId}/signing-secrets", h.IssueSigningSecret)
	r.Get("/{orgId}/signing-secrets", h.ListSigningSecrets)
	return r
}

func TestIssueSigningSecret_Returns201WithAUskPrefixedIDAndTheFullSecretPresentExactlyHere(t *testing.T) {
	r := newSigningSecretTestRouter()

	w := doRequest(r, http.MethodPost, "/org_1/signing-secrets")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto issuedSigningSecretDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !strings.HasPrefix(dto.ID, "usk_") {
		t.Errorf("id = %q, want it to start with %q", dto.ID, "usk_")
	}
	if dto.Secret == "" {
		t.Error("secret is empty, want the freshly minted secret")
	}
	if dto.CreatedAt == "" {
		t.Error("createdAt is empty, want a non-empty timestamp")
	}
}

func TestListSigningSecrets_Returns200WithOnlyIDPrefixAndCreatedAtNeverTheSecret(t *testing.T) {
	r := newSigningSecretTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/signing-secrets")
	var issued issuedSigningSecretDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	doRequest(r, http.MethodPost, "/org_1/signing-secrets")

	w := doRequest(r, http.MethodGet, "/org_1/signing-secrets")

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
	if strings.Contains(w.Body.String(), issued.Secret) {
		t.Fatalf("list response %s contains the raw issued secret %q — it must never be recoverable after issue", w.Body.String(), issued.Secret)
	}
	for _, entry := range entries {
		for _, unwantedField := range []string{"secret", "encryptedSecret"} {
			if _, present := entry[unwantedField]; present {
				t.Errorf("list entry %+v carries a %q field — only id, prefix, and createdAt may appear", entry, unwantedField)
			}
		}
		for _, wantField := range []string{"id", "prefix", "createdAt"} {
			if _, present := entry[wantField]; !present {
				t.Errorf("list entry %+v is missing expected field %q", entry, wantField)
			}
		}
	}
}

func TestListSigningSecrets_OnlyReturnsSecretsBelongingToTheRequestedOrg(t *testing.T) {
	r := newSigningSecretTestRouter()
	doRequest(r, http.MethodPost, "/org_1/signing-secrets")
	doRequest(r, http.MethodPost, "/org_2/signing-secrets")

	w := doRequest(r, http.MethodGet, "/org_1/signing-secrets")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var entries []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (org_2's signing secret must not leak into org_1's list)", len(entries))
	}
}
