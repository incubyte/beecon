package authmw_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"beecon/internal/access/driving/authmw"
	"beecon/internal/organizations"
)

const validSecret = "beecon_sk_valid-org-secret"
const revokedSecret = "beecon_sk_revoked-org-secret"
const validOrg = organizations.OrgID("org_1")

// fakeVerify stands in for (*access.Facade).Verify: authmw depends on the
// Verify func type, not the concrete access.Facade, so a crafted fake is
// enough to exercise every rejection path without wiring real keys.
func fakeVerify(_ context.Context, secret string) (organizations.OrgID, error) {
	switch secret {
	case validSecret:
		return validOrg, nil
	case revokedSecret:
		return "", errors.New("revoked api key")
	default:
		return "", errors.New("unknown api key")
	}
}

// probeHandler reports what OrgAuth put into the request context, so tests
// can assert the org actually lands there rather than just that the request
// passed through.
func probeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, ok := organizations.OrgIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"orgId": string(org), "hasOrg": ok})
	})
}

func newGuardedProbe() http.Handler {
	return authmw.OrgAuth(fakeVerify)(probeHandler())
}

// wireErrorEnvelope and doRequest are already declared in admin_test.go
// (same package authmw_test); reused here rather than redeclared. doRequest
// there issues a GET, which is fine — OrgAuth's rejection paths don't depend
// on the HTTP method.

func TestOrgAuth_RejectsARequestWithNoAuthorizationHeader(t *testing.T) {
	w := doRequest(newGuardedProbe(), "")

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

func TestOrgAuth_RejectsAMalformedAuthorizationHeaderMissingTheBearerPrefix(t *testing.T) {
	w := doRequest(newGuardedProbe(), validSecret)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestOrgAuth_RejectsAnEmptyBearerToken(t *testing.T) {
	w := doRequest(newGuardedProbe(), "Bearer ")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestOrgAuth_RejectsAnUnknownKey(t *testing.T) {
	w := doRequest(newGuardedProbe(), "Bearer beecon_sk_never-issued")

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

func TestOrgAuth_RejectsARevokedKey(t *testing.T) {
	w := doRequest(newGuardedProbe(), "Bearer "+revokedSecret)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestOrgAuth_PassesThroughAndInjectsTheVerifiedOrgIDIntoContextForAValidKey(t *testing.T) {
	w := doRequest(newGuardedProbe(), "Bearer "+validSecret)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		OrgID  string `json:"orgId"`
		HasOrg bool   `json:"hasOrg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode probe response: %v; body=%s", err, w.Body.String())
	}
	if !body.HasOrg {
		t.Fatal("expected OrgIDFromContext to report ok=true after OrgAuth passed the request through")
	}
	if body.OrgID != string(validOrg) {
		t.Errorf("orgId in context = %q, want %q", body.OrgID, validOrg)
	}
}
