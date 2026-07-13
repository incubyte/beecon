// Package httpapi (in-package test; see handler_test.go for the shared
// newTestRouter/doRequest/wireErrorEnvelope/decodeError helpers reused here —
// newTestRouter already mounts Rotate at the same route shape app/router.go
// uses). This file exercises Slice 8/PD23's rotate endpoint: the response
// shape, the optional overlapHours body, and its error paths.
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/httpx"
)

// newRotateTestRouterWithClock is newTestRouter's shape, but with a fixed
// clock injected so overlapExpiresAt can be asserted against an exact
// expected instant instead of merely "non-empty".
func newRotateTestRouterWithClock(now time.Time) chi.Router {
	facade := memory.NewFacadeWithOverrides(memory.Overrides{Now: func() time.Time { return now }})
	errorRenderer := httpx.NewErrorRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/{orgId}/api-keys", h.Issue)
	r.Post("/{orgId}/api-keys/{keyId}/rotate", h.Rotate)
	return r
}

func doRequestWithBody(r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRotate_Returns201WithIDKeyPrefixAndOverlapExpiresAt(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := newRotateTestRouterWithClock(fixedNow)
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/"+issued.ID+"/rotate", "")

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto rotatedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.ID != issued.ID {
		t.Errorf("id = %q, want the same key id %q", dto.ID, issued.ID)
	}
	if dto.Key == issued.Key {
		t.Fatal("rotate returned the same secret Issue did — it must mint a fresh one")
	}
	if !strings.HasPrefix(dto.Key, access.SecretPrefix) {
		t.Errorf("key = %q, want it to start with %q", dto.Key, access.SecretPrefix)
	}
	if len(dto.Prefix) != access.LookupPrefixLength {
		t.Errorf("prefix = %q (len %d), want length %d", dto.Prefix, len(dto.Prefix), access.LookupPrefixLength)
	}
	wantOverlapExpiresAt := fixedNow.Add(access.DefaultOverlapHours * time.Hour).Format(rfc3339Millis)
	if dto.OverlapExpiresAt != wantOverlapExpiresAt {
		t.Errorf("overlapExpiresAt = %q, want %q (the default %dh overlap)", dto.OverlapExpiresAt, wantOverlapExpiresAt, access.DefaultOverlapHours)
	}
}

func TestRotate_AcceptsACustomOverlapHoursInTheRequestBody(t *testing.T) {
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := newRotateTestRouterWithClock(fixedNow)
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/"+issued.ID+"/rotate", `{"overlapHours":2}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var dto rotatedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	wantOverlapExpiresAt := fixedNow.Add(2 * time.Hour).Format(rfc3339Millis)
	if dto.OverlapExpiresAt != wantOverlapExpiresAt {
		t.Errorf("overlapExpiresAt = %q, want %q (the requested 2h overlap, not the %dh default)", dto.OverlapExpiresAt, wantOverlapExpiresAt, access.DefaultOverlapHours)
	}
}

func TestRotate_ReturnsValidationFailedForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/"+issued.ID+"/rotate", "not-json-at-all")

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeValidationFailed {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeValidationFailed)
	}
}

func TestRotate_Returns404ForAnUnknownKeyID(t *testing.T) {
	r := newTestRouter()

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/key_missing/rotate", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeNotFound {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeNotFound)
	}
}

func TestRotate_Returns404ForAKeyBelongingToAnotherOrg(t *testing.T) {
	r := newTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_2/api-keys/"+issued.ID+"/rotate", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestRotate_ReturnsValidationFailedForANegativeOverlapHours is the HTTP-level
// half of the facade's negative-overlapHours guard (access.rotate_test.go
// covers the facade directly): a negative overlapHours is a 422
// validation_failed, the same PD5 shape a malformed body produces.
func TestRotate_ReturnsValidationFailedForANegativeOverlapHours(t *testing.T) {
	r := newTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/"+issued.ID+"/rotate", `{"overlapHours":-1}`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeValidationFailed {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeValidationFailed)
	}
}

// TestRotate_Returns404ForAnAlreadyRevokedKey is the HTTP-level half of the
// facade's revoked-key guard (access.rotate_test.go's
// TestRotate_OnAnAlreadyRevokedKeyIsRejectedAsNotFound covers the facade
// directly): rotating a key that was already revoked through the same admin
// endpoints surfaces as the standard PD5 not-found shape, exactly like an
// unknown or cross-org key id.
func TestRotate_Returns404ForAnAlreadyRevokedKey(t *testing.T) {
	r := newTestRouter()
	issueResp := doRequest(r, http.MethodPost, "/org_1/api-keys")
	var issued issuedKeyDTO
	if err := json.Unmarshal(issueResp.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	revokeResp := doRequest(r, http.MethodDelete, "/org_1/api-keys/"+issued.ID)
	if revokeResp.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want %d; body=%s", revokeResp.Code, http.StatusNoContent, revokeResp.Body.String())
	}

	w := doRequestWithBody(r, http.MethodPost, "/org_1/api-keys/"+issued.ID+"/rotate", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Code != access.CodeNotFound {
		t.Errorf("error.code = %q, want %q", env.Error.Code, access.CodeNotFound)
	}
}
