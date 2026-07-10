package authmw_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/access/driving/authmw"
)

const testAdminKey = "beecon-admin-secret"

func newProtectedHandler() http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"passed":true}`))
	})
	return authmw.AdminAuth(testAdminKey)(next)
}

type wireErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func doRequest(h http.Handler, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAdminAuth_RejectsARequestWithNoAuthorizationHeader(t *testing.T) {
	w := doRequest(newProtectedHandler(), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestAdminAuth_RejectsAWrongAdminKey(t *testing.T) {
	w := doRequest(newProtectedHandler(), "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestAdminAuth_RejectsAMalformedAuthorizationHeaderMissingTheBearerPrefix(t *testing.T) {
	w := doRequest(newProtectedHandler(), testAdminKey)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestAdminAuth_RejectsAnEmptyBearerToken(t *testing.T) {
	w := doRequest(newProtectedHandler(), "Bearer ")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestAdminAuth_PassesThroughWithTheCorrectAdminKey(t *testing.T) {
	w := doRequest(newProtectedHandler(), "Bearer "+testAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != `{"passed":true}` {
		t.Errorf("body = %s, want the wrapped handler's own response to pass through untouched", w.Body.String())
	}
}
