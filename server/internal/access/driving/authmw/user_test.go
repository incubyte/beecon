package authmw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/access/driving/authmw"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const validUserToken = "valid-user-token"
const expiredUserToken = "expired-user-token"
const validUserOrg = organizations.OrgID("org_2")
const validUserID = organizations.UserID("user_ada")

// fakeVerifyUserToken stands in for (*access.Facade).VerifyUserToken: authmw
// depends on the VerifyUserToken func type, not the concrete access.Facade,
// so a crafted fake exercises every rejection path without minting real
// HS256 JWTs (usertoken_test.go in package access_test already covers the
// real cryptographic tamper/wrong-secret/expired matrix). Every business
// rejection returns a *httpx.DomainError (httpx.Unauthorized) — exactly what
// the real access.Facade.VerifyUserToken returns for an expired or otherwise
// invalid token (access/usertoken.go's own ErrUnauthorized() calls) — never a
// plain error, which authmw's own PD38b logic (autherror.go) would instead
// treat as an infrastructure failure and render as 500.
func fakeVerifyUserToken(_ context.Context, token string) (organizations.OrgID, organizations.UserID, error) {
	switch token {
	case validUserToken:
		return validUserOrg, validUserID, nil
	case expiredUserToken:
		return "", "", httpx.Unauthorized("expired user token")
	default:
		return "", "", httpx.Unauthorized("invalid user token")
	}
}

func userProbeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		org, hasOrg := organizations.OrgIDFromContext(r.Context())
		user, hasUser := organizations.UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"orgId": string(org), "hasOrg": hasOrg,
			"userId": string(user), "hasUser": hasUser,
		})
	})
}

type userProbeBody struct {
	OrgID   string `json:"orgId"`
	HasOrg  bool   `json:"hasOrg"`
	UserID  string `json:"userId"`
	HasUser bool   `json:"hasUser"`
}

func decodeUserProbe(t *testing.T, w *httptest.ResponseRecorder) userProbeBody {
	t.Helper()
	var body userProbeBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode probe response: %v; body=%s", err, w.Body.String())
	}
	return body
}

func newGuardedUserProbe() http.Handler {
	return authmw.UserAuth(fakeVerifyUserToken)(userProbeHandler())
}

func TestUserAuth_RejectsARequestWithNoAuthorizationHeader(t *testing.T) {
	w := doRequest(newGuardedUserProbe(), "")

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

func TestUserAuth_RejectsAMalformedAuthorizationHeaderMissingTheBearerPrefix(t *testing.T) {
	w := doRequest(newGuardedUserProbe(), validUserToken)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestUserAuth_RejectsAnEmptyBearerToken(t *testing.T) {
	w := doRequest(newGuardedUserProbe(), "Bearer ")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestUserAuth_RejectsAnExpiredToken(t *testing.T) {
	w := doRequest(newGuardedUserProbe(), "Bearer "+expiredUserToken)

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

func TestUserAuth_PassesThroughAndInjectsOrgAndUserIntoContextForAValidToken(t *testing.T) {
	w := doRequest(newGuardedUserProbe(), "Bearer "+validUserToken)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeUserProbe(t, w)
	if !body.HasOrg || body.OrgID != string(validUserOrg) {
		t.Errorf("orgId = %q (hasOrg=%v), want %q", body.OrgID, body.HasOrg, validUserOrg)
	}
	if !body.HasUser || body.UserID != string(validUserID) {
		t.Errorf("userId = %q (hasUser=%v), want %q", body.UserID, body.HasUser, validUserID)
	}
}

// --- OrgOrUser ---

func newGuardedOrgOrUserProbe() http.Handler {
	return authmw.OrgOrUser(fakeVerify, fakeVerifyUserToken)(userProbeHandler())
}

func TestOrgOrUser_RejectsARequestWithNoAuthorizationHeader(t *testing.T) {
	w := doRequest(newGuardedOrgOrUserProbe(), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestOrgOrUser_RejectsATokenThatIsNeitherAValidOrgKeyNorAValidUserToken(t *testing.T) {
	w := doRequest(newGuardedOrgOrUserProbe(), "Bearer complete-nonsense")

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

func TestOrgOrUser_AcceptsAValidOrgKeyAndInjectsOnlyTheOrgIntoContext(t *testing.T) {
	w := doRequest(newGuardedOrgOrUserProbe(), "Bearer "+validSecret)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeUserProbe(t, w)
	if !body.HasOrg || body.OrgID != string(validOrg) {
		t.Errorf("orgId = %q (hasOrg=%v), want %q", body.OrgID, body.HasOrg, validOrg)
	}
	if body.HasUser {
		t.Error("hasUser = true for an org-key request, want false — only a user token carries a UserID")
	}
}

func TestOrgOrUser_AcceptsAValidUserTokenAndInjectsBothOrgAndUserIntoContext(t *testing.T) {
	w := doRequest(newGuardedOrgOrUserProbe(), "Bearer "+validUserToken)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := decodeUserProbe(t, w)
	if !body.HasOrg || body.OrgID != string(validUserOrg) {
		t.Errorf("orgId = %q (hasOrg=%v), want %q", body.OrgID, body.HasOrg, validUserOrg)
	}
	if !body.HasUser || body.UserID != string(validUserID) {
		t.Errorf("userId = %q (hasUser=%v), want %q", body.UserID, body.HasUser, validUserID)
	}
}

func TestOrgOrUser_RejectsARevokedOrgKeyThatIsAlsoNotAValidUserToken(t *testing.T) {
	w := doRequest(newGuardedOrgOrUserProbe(), "Bearer "+revokedSecret)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
