// Package authmw_test (see admin_test.go's own header for the shared
// wireErrorEnvelope/doRequest helpers reused here). This file covers
// RequireWrite (PD41, Slice 4): the middleware that rejects a read-only org
// API key on a mutating route, table-driven over every scope-context shape a
// real request can arrive with.
package authmw_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/access"
	"beecon/internal/access/driving/authmw"
	"beecon/internal/organizations"
)

// newPlainRequest builds a bare GET request with no scope/org/user context
// at all — each test case then layers its own context onto it via
// buildContext, so every RequireWrite scenario starts from the same clean
// slate.
func newPlainRequest() *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/v1/whatever", nil)
}

// doGuardedRequest dispatches req (already carrying whatever context a test
// case built) through h and returns the recorded response — the
// context-carrying counterpart to admin_test.go's doRequest, which always
// builds its own fresh request and so cannot inject a pre-built context.
func doGuardedRequest(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// requireWriteProbeHandler is the innermost handler RequireWrite wraps: it
// only ever runs when RequireWrite passed the request through, so a test
// asserting a 200 here is asserting RequireWrite let it through, not that
// the probe itself did anything special.
func requireWriteProbeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"passed":true}`))
	})
}

func TestRequireWrite_TableDriven(t *testing.T) {
	tests := []struct {
		name string
		// buildContext installs whatever RequireWrite reads via
		// access.ScopeFromContext (or nothing, for the "no scope" cases) onto
		// a bare context before the request is dispatched.
		buildContext func(r *http.Request) *http.Request
		wantStatus   int
	}{
		{
			name: "a read-only org API key's scope in context is rejected with 403",
			buildContext: func(r *http.Request) *http.Request {
				ctx := access.WithScope(r.Context(), access.ScopeReadOnly)
				return r.WithContext(ctx)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "a read-write org API key's scope in context passes through",
			buildContext: func(r *http.Request) *http.Request {
				ctx := access.WithScope(r.Context(), access.ScopeReadWrite)
				return r.WithContext(ctx)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "no scope in context at all (an admin-key request) passes through untouched",
			buildContext: func(r *http.Request) *http.Request {
				return r
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "no scope in context, but an org id is present (a user-token request on an orgOrUser route) still passes through",
			buildContext: func(r *http.Request) *http.Request {
				ctx := organizations.WithOrgID(r.Context(), organizations.OrgID("org_1"))
				ctx = organizations.WithUserID(ctx, organizations.UserID("user_1"))
				return r.WithContext(ctx)
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			guarded := authmw.RequireWrite(requireWriteProbeHandler())
			req := tc.buildContext(newPlainRequest())

			w := doGuardedRequest(guarded, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// TestRequireWrite_RejectionCarriesTheForbiddenCodeAndAScopeExplainingMessage
// pins the wire shape of RequireWrite's 403 beyond the bare status code: the
// PD5 envelope's machine-readable "forbidden" code (distinct from
// "unauthorized" — the credential authenticated fine, it just isn't
// permitted this action) and a message that actually explains why, so an
// operator reading the response understands it's a scope problem rather than
// a generic denial.
func TestRequireWrite_RejectionCarriesTheForbiddenCodeAndAScopeExplainingMessage(t *testing.T) {
	guarded := authmw.RequireWrite(requireWriteProbeHandler())
	req := newPlainRequest()
	ctx := access.WithScope(req.Context(), access.ScopeReadOnly)
	req = req.WithContext(ctx)

	w := doGuardedRequest(guarded, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if env.Error.Code != "forbidden" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "forbidden")
	}
	if env.Error.Message == "" {
		t.Error("error.message is empty, want a scope-explaining message")
	}
}

// TestRequireWrite_APassedThroughRequestReachesTheWrappedHandlersOwnResponse
// confirms RequireWrite doesn't just return 200 on its own — the wrapped
// handler's actual response body comes through untouched.
func TestRequireWrite_APassedThroughRequestReachesTheWrappedHandlersOwnResponse(t *testing.T) {
	guarded := authmw.RequireWrite(requireWriteProbeHandler())
	req := newPlainRequest()
	ctx := access.WithScope(req.Context(), access.ScopeReadWrite)
	req = req.WithContext(ctx)

	w := doGuardedRequest(guarded, req)

	if w.Body.String() != `{"passed":true}` {
		t.Errorf("body = %s, want the wrapped handler's own response to pass through untouched", w.Body.String())
	}
}
