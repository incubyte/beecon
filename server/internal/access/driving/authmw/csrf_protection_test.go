// Package authmw_test (see console_auth_test.go for the stubVerifySession/
// sessionCookie/operatorsExistFunc/newConsoleAuthHandler/
// newOperatorSessionHandler fixtures reused here). This file covers Phase 5
// Slice 3's CSRF branch of ConsoleAuth and OperatorSession (PD52, architecture
// doc §3): the double-submit X-CSRF-Token check on session-authenticated
// mutating requests, its exemption on safe methods and the admin-key Bearer
// branch, its binding to the presented session specifically (not the
// operator, not any session), and that a rejection never echoes the expected
// token value.
package authmw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/access"
)

// doCSRFRequest builds a request against the given method (mutating or
// safe), optionally carrying a session cookie, an X-CSRF-Token header, and/or
// an Authorization header.
func doCSRFRequest(h http.Handler, method string, cookie *http.Cookie, csrfHeader, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/organizations", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrfHeader != "" {
		req.Header.Set("X-CSRF-Token", csrfHeader)
	}
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

var mutatingMethodsUnderTest = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

const csrfProtectionTestSessionToken = "csrf-test-session-token"
const csrfProtectionTestCSRFToken = "the-session-own-csrf-token"

// --- ConsoleAuth's session branch: CSRF required on every mutating method. ---

func TestConsoleAuth_RejectsASessionAuthenticatedMutationWithNoCSRFTokenOnEveryMutatingMethod(t *testing.T) {
	for _, method := range mutatingMethodsUnderTest {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), "", "")

			if w.Code != http.StatusForbidden {
				t.Fatalf("%s with no X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusForbidden, w.Body.String())
			}
			env := decodeWireError(t, w.Body.Bytes())
			if env.Error.Code != "csrf_failed" {
				t.Errorf("%s: error.code = %q, want %q", method, env.Error.Code, "csrf_failed")
			}
		})
	}
}

func TestConsoleAuth_RejectsASessionAuthenticatedMutationWithAWrongCSRFTokenOnEveryMutatingMethod(t *testing.T) {
	for _, method := range mutatingMethodsUnderTest {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), "a-completely-wrong-token", "")

			if w.Code != http.StatusForbidden {
				t.Fatalf("%s with a wrong X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusForbidden, w.Body.String())
			}
		})
	}
}

func TestConsoleAuth_PassesASessionAuthenticatedMutationWithTheCorrectCSRFTokenOnEveryMutatingMethod(t *testing.T) {
	for _, method := range mutatingMethodsUnderTest {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), csrfProtectionTestCSRFToken, "")

			if w.Code != http.StatusOK {
				t.Fatalf("%s with the correct X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusOK, w.Body.String())
			}
			if w.Body.String() != "operator:op_1" {
				t.Errorf("%s: body = %q, want the operator still injected into context", method, w.Body.String())
			}
		})
	}
}

// TestConsoleAuth_SafeMethodsRequireNoCSRFTokenEvenWhenSessionAuthenticated
// is Slice 3 AC2: a session-authenticated GET/HEAD passes with no
// X-CSRF-Token header at all.
func TestConsoleAuth_SafeMethodsRequireNoCSRFTokenEvenWhenSessionAuthenticated(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), "", "")

			if w.Code != http.StatusOK {
				t.Fatalf("%s with no X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

// TestConsoleAuth_RejectsACSRFTokenIssuedForADifferentSession is Slice 3 AC3
// (the token is bound to the session, not the operator or any session in
// general): session B's own CSRF token, presented on a request authenticated
// as session B, is what matters — session A's token never satisfies it, even
// though both sessions might belong to the same operator.
func TestConsoleAuth_RejectsACSRFTokenIssuedForADifferentSession(t *testing.T) {
	verify := func(_ context.Context, token string) (access.AuthenticatedOperator, error) {
		switch token {
		case "session-a-token":
			return access.AuthenticatedOperator{OperatorID: "op_a", CSRFToken: "csrf-issued-for-session-a"}, nil
		case "session-b-token":
			return access.AuthenticatedOperator{OperatorID: "op_b", CSRFToken: "csrf-issued-for-session-b"}, nil
		default:
			return access.AuthenticatedOperator{}, access.ErrSessionUnauthorized()
		}
	}
	h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

	// Authenticated as session B, but presenting session A's CSRF token.
	w := doCSRFRequest(h, http.MethodPost, sessionCookie("session-b-token"), "csrf-issued-for-session-a", "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d — a CSRF token issued for a different session must never satisfy this session's check; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}

	// Confirm the reverse also passes with each session's own token, so the
	// rejection above is really about cross-session binding and not some
	// other fixture bug.
	wOwnTokenA := doCSRFRequest(h, http.MethodPost, sessionCookie("session-a-token"), "csrf-issued-for-session-a", "")
	if wOwnTokenA.Code != http.StatusOK {
		t.Fatalf("session A with its own token: status = %d, want %d", wOwnTokenA.Code, http.StatusOK)
	}
	wOwnTokenB := doCSRFRequest(h, http.MethodPost, sessionCookie("session-b-token"), "csrf-issued-for-session-b", "")
	if wOwnTokenB.Code != http.StatusOK {
		t.Fatalf("session B with its own token: status = %d, want %d", wOwnTokenB.Code, http.StatusOK)
	}
}

// TestConsoleAuth_TheAdminKeyBranchAcceptsAMutatingRequestWithNoCSRFTokenAtAll
// pins the deliberate exemption: the pre-bootstrap admin-key Bearer branch is
// not cookie-borne, so a forged cross-site request can never carry it — CSRF
// protection would be meaningless there, and this is why the Phase 4
// Bearer-authenticated console journeys stay green through Slice 3.
func TestConsoleAuth_TheAdminKeyBranchAcceptsAMutatingRequestWithNoCSRFTokenAtAll(t *testing.T) {
	verify := stubVerifySession("never-presented", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doCSRFRequest(h, http.MethodPost, nil, "", "Bearer "+consoleAuthTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — the admin-key Bearer branch must never be CSRF-checked; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestConsoleAuth_ACSRFRejectionNeverContainsTheExpectedTokenValue is Slice 3
// AC6: the 403 response must never leak the session's own CSRF token, in the
// body or in any response header.
func TestConsoleAuth_ACSRFRejectionNeverContainsTheExpectedTokenValue(t *testing.T) {
	const secretSessionCSRFToken = "super-secret-session-csrf-value-must-never-leak"
	verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: secretSessionCSRFToken}, nil)
	h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

	w := doCSRFRequest(h, http.MethodPost, sessionCookie(csrfProtectionTestSessionToken), "attacker-guess", "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secretSessionCSRFToken) {
		t.Errorf("response body leaked the expected CSRF token value: %s", w.Body.String())
	}
	for name, values := range w.Header() {
		for _, v := range values {
			if strings.Contains(v, secretSessionCSRFToken) {
				t.Errorf("response header %q leaked the expected CSRF token value: %s", name, v)
			}
		}
	}
	var body struct {
		Error struct {
			Message string          `json:"message"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v; body=%s", err, w.Body.String())
	}
	if body.Error.Message == "" || strings.Contains(body.Error.Message, secretSessionCSRFToken) {
		t.Errorf("error.message = %q, want a generic message never containing the token", body.Error.Message)
	}
}

// --- OperatorSession: the same CSRF branch, guarding /auth/logout among
// others (Slice 3 AC5's logout half). ---

func TestOperatorSession_RejectsAMutationWithNoCSRFTokenOnEveryMutatingMethod(t *testing.T) {
	for _, method := range mutatingMethodsUnderTest {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newOperatorSessionHandler(verify)

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), "", "")

			if w.Code != http.StatusForbidden {
				t.Fatalf("%s with no X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusForbidden, w.Body.String())
			}
		})
	}
}

func TestOperatorSession_RejectsAMutationWithAWrongCSRFToken(t *testing.T) {
	verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
	h := newOperatorSessionHandler(verify)

	w := doCSRFRequest(h, http.MethodPost, sessionCookie(csrfProtectionTestSessionToken), "wrong-token", "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

// TestOperatorSession_PassesAMutationWithTheCorrectCSRFToken is the direct
// "logout with the token succeeds" assertion (Slice 3 AC5) at the middleware
// level — router.go mounts POST /auth/logout behind exactly this middleware.
func TestOperatorSession_PassesAMutationWithTheCorrectCSRFToken(t *testing.T) {
	for _, method := range mutatingMethodsUnderTest {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newOperatorSessionHandler(verify)

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), csrfProtectionTestCSRFToken, "")

			if w.Code != http.StatusOK {
				t.Fatalf("%s with the correct X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestOperatorSession_SafeMethodsRequireNoCSRFTokenEvenWhenSessionAuthenticated(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: csrfProtectionTestCSRFToken}, nil)
			h := newOperatorSessionHandler(verify)

			w := doCSRFRequest(h, method, sessionCookie(csrfProtectionTestSessionToken), "", "")

			if w.Code != http.StatusOK {
				t.Fatalf("%s with no X-CSRF-Token: status = %d, want %d; body=%s", method, w.Code, http.StatusOK, w.Body.String())
			}
		})
	}
}

func TestOperatorSession_ACSRFRejectionNeverContainsTheExpectedTokenValue(t *testing.T) {
	const secretSessionCSRFToken = "super-secret-operator-session-csrf-value"
	verify := stubVerifySession(csrfProtectionTestSessionToken, access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: secretSessionCSRFToken}, nil)
	h := newOperatorSessionHandler(verify)

	w := doCSRFRequest(h, http.MethodPost, sessionCookie(csrfProtectionTestSessionToken), "attacker-guess", "")

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	if strings.Contains(w.Body.String(), secretSessionCSRFToken) {
		t.Errorf("response body leaked the expected CSRF token value: %s", w.Body.String())
	}
}
