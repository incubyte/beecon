// Package authmw_test (see admin_test.go's own header for the doRequest/
// wireErrorEnvelope conventions reused here). This file covers Phase 5
// Slice 1's ConsoleAuth and OperatorSession middlewares (FD-A, architecture
// doc §3): session-first authentication, the admin key's pre-bootstrap
// break-glass window and its live demotion once an operator exists (Slice 4's
// AC8, wired from Slice 1 on), and the CSRF hook's deliberate no-op this
// slice.
package authmw_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/access"
	"beecon/internal/access/driving/authmw"
)

const consoleAuthTestAdminKey = "console-auth-admin-secret"

const consoleAuthTestOperatorID = access.OperatorID("op_1")

var errConsoleAuthSessionLookupFailed = errors.New("session lookup infrastructure failure")

// stubVerifySession returns a authmw.VerifySession that succeeds for
// wantToken (returning operator), and otherwise fails with err (an
// *httpx.DomainError to mimic a real rejection, or a plain error to mimic an
// infrastructure failure).
func stubVerifySession(wantToken string, operator access.AuthenticatedOperator, err error) authmw.VerifySession {
	return func(_ context.Context, token string) (access.AuthenticatedOperator, error) {
		if token == wantToken {
			return operator, nil
		}
		return access.AuthenticatedOperator{}, err
	}
}

func newConsoleAuthHandler(verify authmw.VerifySession, operatorsExist func(context.Context) (bool, error)) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operatorID, ok := access.OperatorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		if ok {
			_, _ = w.Write([]byte("operator:" + string(operatorID)))
		} else {
			_, _ = w.Write([]byte("no-operator-in-context"))
		}
	})
	return authmw.ConsoleAuth(verify, consoleAuthTestAdminKey, operatorsExist)(next)
}

func doConsoleAuthRequest(h http.Handler, cookie *http.Cookie, authorizationHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if authorizationHeader != "" {
		req.Header.Set("Authorization", authorizationHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func sessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: authmw.SessionCookieName, Value: token}
}

func operatorsExistFunc(exists bool) func(context.Context) (bool, error) {
	return func(context.Context) (bool, error) { return exists, nil }
}

// --- ConsoleAuth: session branch. ---

func TestConsoleAuth_AValidSessionCookieAuthenticatesAndInjectsTheOperator(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: "csrf"}, nil)
	h := newConsoleAuthHandler(verify, operatorsExistFunc(true))

	w := doConsoleAuthRequest(h, sessionCookie("valid-token"), "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != "operator:op_1" {
		t.Errorf("body = %q, want the injected operator id to reach the wrapped handler via access.OperatorFromContext", w.Body.String())
	}
}

func TestConsoleAuth_AnInvalidSessionCookieIsRejectedEvenWhenNoOperatorExistsYet(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doConsoleAuthRequest(h, sessionCookie("some-other-token"), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestConsoleAuth_ASessionLookupInfrastructureFailureRendersAsFiveHundredNotUnauthorized(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{}, errConsoleAuthSessionLookupFailed)
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doConsoleAuthRequest(h, sessionCookie("valid-token-but-lookup-errors"), "")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (a plain infrastructure error must never render as a 401 verdict on the credential); body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
}

// --- ConsoleAuth: admin-key branch (the live-demotion AC). ---

func TestConsoleAuth_TheAdminKeyAuthenticatesWhenNoSessionCookieIsPresentAndNoOperatorExistsYet(t *testing.T) {
	verify := stubVerifySession("never-presented", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doConsoleAuthRequest(h, nil, "Bearer "+consoleAuthTestAdminKey)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (pre-bootstrap break-glass window); body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Body.String() != "no-operator-in-context" {
		t.Errorf("body = %q, want no operator injected for an admin-key-authenticated request", w.Body.String())
	}
}

// TestConsoleAuth_TheAdminKeyIsRejectedOnceAnOperatorAccountExists is the
// direct AC8/PD54 assertion: the exact same admin key that authenticated a
// moment ago (operatorsExist=false) is rejected the instant operatorsExist
// flips to true — the live-demotion behavior, driven both ways rather than
// asserted from only one state.
func TestConsoleAuth_TheAdminKeyIsRejectedOnceAnOperatorAccountExists(t *testing.T) {
	verify := stubVerifySession("never-presented", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))
	preBootstrap := doConsoleAuthRequest(h, nil, "Bearer "+consoleAuthTestAdminKey)
	if preBootstrap.Code != http.StatusOK {
		t.Fatalf("pre-bootstrap: status = %d, want %d — test fixture bug: the admin key should still work before any operator exists", preBootstrap.Code, http.StatusOK)
	}

	hPostBootstrap := newConsoleAuthHandler(verify, operatorsExistFunc(true))
	postBootstrap := doConsoleAuthRequest(hPostBootstrap, nil, "Bearer "+consoleAuthTestAdminKey)

	if postBootstrap.Code != http.StatusUnauthorized {
		t.Fatalf("post-bootstrap: status = %d, want %d — the admin key must no longer authenticate the general console once an operator account exists", postBootstrap.Code, http.StatusUnauthorized)
	}
}

func TestConsoleAuth_RejectsAWrongAdminKeyWithNoSessionCookie(t *testing.T) {
	verify := stubVerifySession("never-presented", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doConsoleAuthRequest(h, nil, "Bearer wrong-key")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestConsoleAuth_RejectsARequestWithNeitherASessionCookieNorAnAdminKey(t *testing.T) {
	verify := stubVerifySession("never-presented", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))

	w := doConsoleAuthRequest(h, nil, "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestConsoleAuth_ASessionCookiePreemptsTheAdminKeyEvenWhenBothArePresent(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: "csrf"}, nil)
	h := newConsoleAuthHandler(verify, operatorsExistFunc(false))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/organizations", nil)
	req.AddCookie(sessionCookie("valid-token"))
	req.Header.Set("Authorization", "Bearer wrong-admin-key-entirely")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — a valid session cookie must authenticate regardless of an accompanying (even wrong) Authorization header", w.Code, http.StatusOK)
	}
}

// --- OperatorSession (session-only; no break-glass fallback at all). ---

func newOperatorSessionHandler(verify authmw.VerifySession) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operatorID, ok := access.OperatorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
		if ok {
			_, _ = w.Write([]byte("operator:" + string(operatorID)))
		}
	})
	return authmw.OperatorSession(verify)(next)
}

func TestOperatorSession_AuthenticatesAndInjectsTheOperatorForAValidSessionCookie(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: "csrf"}, nil)
	h := newOperatorSessionHandler(verify)

	w := doConsoleAuthRequest(h, sessionCookie("valid-token"), "")

	if w.Code != http.StatusOK || w.Body.String() != "operator:op_1" {
		t.Fatalf("status=%d body=%q, want 200 and the injected operator id", w.Code, w.Body.String())
	}
}

func TestOperatorSession_RejectsARequestWithNoSessionCookieAtAll(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: "csrf"}, nil)
	h := newOperatorSessionHandler(verify)

	w := doConsoleAuthRequest(h, nil, "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

// TestOperatorSession_NeverAcceptsTheAdminKeyEvenWhenNoOperatorExistsYet is
// the explicit contrast with ConsoleAuth's own pre-bootstrap admin-key
// branch: OperatorSession (guarding /auth/me, /auth/logout, /operators/*)
// has no break-glass fallback at all, so a bare Bearer admin key never
// authenticates it, regardless of operatorsExist.
func TestOperatorSession_NeverAcceptsTheAdminKeyEvenWhenNoOperatorExistsYet(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{OperatorID: consoleAuthTestOperatorID, CSRFToken: "csrf"}, nil)
	h := newOperatorSessionHandler(verify)

	w := doConsoleAuthRequest(h, nil, "Bearer "+consoleAuthTestAdminKey)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d — OperatorSession must never accept the admin key", w.Code, http.StatusUnauthorized)
	}
}

func TestOperatorSession_RejectsAnInvalidSessionToken(t *testing.T) {
	verify := stubVerifySession("valid-token", access.AuthenticatedOperator{}, access.ErrSessionUnauthorized())
	h := newOperatorSessionHandler(verify)

	w := doConsoleAuthRequest(h, sessionCookie("some-other-token"), "")

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}
