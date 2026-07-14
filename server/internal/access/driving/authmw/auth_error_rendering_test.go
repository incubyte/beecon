package authmw_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"beecon/internal/access/driving/authmw"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// This file pins PD38b's own core behavior directly, independent of any
// fixture in org_test.go/user_test.go: a verify function's plain error (a
// stand-in for a database failure or any other infrastructure problem) must
// always render as the PD5 500 internal_error envelope, while a
// *httpx.DomainError (what every real business rejection returns) must
// render as-is — its own status and code, unauthorized among them, but not
// exclusively.

// infraFailure is a plain (non-DomainError) error — the stand-in for "the
// database is down" or any other verification-time infrastructure problem.
var infraFailure = errors.New("database is down")

func fakeVerifyAlwaysInfraFailure(context.Context, string) (organizations.OrgID, error) {
	return "", infraFailure
}

func fakeVerifyAlwaysUnauthorized(context.Context, string) (organizations.OrgID, error) {
	return "", httpx.Unauthorized("invalid or revoked api key")
}

func fakeVerifyUserTokenAlwaysInfraFailure(context.Context, string) (organizations.OrgID, organizations.UserID, error) {
	return "", "", infraFailure
}

func fakeVerifyUserTokenAlwaysUnauthorized(context.Context, string) (organizations.OrgID, organizations.UserID, error) {
	return "", "", httpx.Unauthorized("invalid or expired user token")
}

func decodeWireError(t *testing.T, body []byte) wireErrorEnvelope {
	t.Helper()
	var env wireErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, body)
	}
	return env
}

func TestOrgAuth_RendersAnInfrastructureFailureDuringVerificationAs500(t *testing.T) {
	guarded := authmw.OrgAuth(fakeVerifyAlwaysInfraFailure)(probeHandler())

	w := doRequest(guarded, "Bearer beecon_sk_whatever")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if env := decodeWireError(t, w.Body.Bytes()); env.Error.Code == "unauthorized" {
		t.Errorf("error.code = %q, want anything but that — an infrastructure failure is not a verdict on the credential", env.Error.Code)
	}
}

func TestOrgAuth_RendersABusinessRejectionDuringVerificationAsUnauthorized(t *testing.T) {
	guarded := authmw.OrgAuth(fakeVerifyAlwaysUnauthorized)(probeHandler())

	w := doRequest(guarded, "Bearer beecon_sk_whatever")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if env := decodeWireError(t, w.Body.Bytes()); env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestUserAuth_RendersAnInfrastructureFailureDuringVerificationAs500(t *testing.T) {
	guarded := authmw.UserAuth(fakeVerifyUserTokenAlwaysInfraFailure)(userProbeHandler())

	w := doRequest(guarded, "Bearer whatever-token")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if env := decodeWireError(t, w.Body.Bytes()); env.Error.Code == "unauthorized" {
		t.Errorf("error.code = %q, want anything but that — an infrastructure failure is not a verdict on the credential", env.Error.Code)
	}
}

func TestUserAuth_RendersABusinessRejectionDuringVerificationAsUnauthorized(t *testing.T) {
	guarded := authmw.UserAuth(fakeVerifyUserTokenAlwaysUnauthorized)(userProbeHandler())

	w := doRequest(guarded, "Bearer whatever-token")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if env := decodeWireError(t, w.Body.Bytes()); env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}

func TestOrgOrUser_RendersAnInfrastructureFailureAs500WhenTheOrgPathFailsThatWayEvenThoughTheUserPathIsAnOrdinaryRejection(t *testing.T) {
	guarded := authmw.OrgOrUser(fakeVerifyAlwaysInfraFailure, fakeVerifyUserTokenAlwaysUnauthorized)(userProbeHandler())

	w := doRequest(guarded, "Bearer whatever-credential")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestOrgOrUser_RendersAnInfrastructureFailureAs500WhenTheUserPathFailsThatWayEvenThoughTheOrgPathIsAnOrdinaryRejection(t *testing.T) {
	guarded := authmw.OrgOrUser(fakeVerifyAlwaysUnauthorized, fakeVerifyUserTokenAlwaysInfraFailure)(userProbeHandler())

	w := doRequest(guarded, "Bearer whatever-credential")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

func TestOrgOrUser_RendersUnauthorizedOnlyWhenBothPathsRejectAsOrdinaryBusinessRejections(t *testing.T) {
	guarded := authmw.OrgOrUser(fakeVerifyAlwaysUnauthorized, fakeVerifyUserTokenAlwaysUnauthorized)(userProbeHandler())

	w := doRequest(guarded, "Bearer whatever-credential")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
	if env := decodeWireError(t, w.Body.Bytes()); env.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
	}
}
