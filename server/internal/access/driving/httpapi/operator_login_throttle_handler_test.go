// Package httpapi (in-package test, see handler_test.go's own header for
// why). This file covers Phase 5 Slice 5's Login brute-force lockout at the
// wire level: newOperatorTestFacade (operator_handler_test.go) wires the
// facade with no WithLoginThrottle override, so it falls back to the same
// default threshold (5) and cooldown (15m) production config defaults to —
// the point of these tests is confirming the 429 actually reaches the HTTP
// response with the right status and a generic body, not re-proving the
// lockout arithmetic itself (operator_login_throttle_test.go, package
// access_test, already covers that exhaustively with an injected clock).
package httpapi

import (
	"strings"
	"testing"
)

func TestOperatorHandlerLogin_Returns429OnceTheDefaultAttemptThresholdIsCrossed(t *testing.T) {
	h := newOperatorTestHandler(t, false)
	bootstrapViaHandler(t, h)
	const defaultMaxAttempts = 5
	for i := 0; i < defaultMaxAttempts; i++ {
		doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"totally-wrong-password"}`)
	}

	w := doOperatorHandlerRequest(h.Login, `{"email":"operator@example.com","password":"correct horse battery staple"}`)

	if w.Code != 429 {
		t.Fatalf("status = %d, want 429; body=%s", w.Code, w.Body.String())
	}
	env := decodeError(t, w)
	if env.Error.Message != "too many failed attempts, try again later" {
		t.Errorf("error.message = %q, want the fixed generic lockout message", env.Error.Message)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "operator@example.com") {
		t.Fatal("the 429 response body mentions the operator's email — the lockout response must never confirm which account exists")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("expected no session cookies to be set on a locked-out login attempt")
	}
}
