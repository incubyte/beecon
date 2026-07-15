//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses doOperatorAuthRequest, declared in
// operator_auth_bootstrap_and_login_journey_integration_test.go, and
// wireErrorEnvelope, declared in organizations_journey_integration_test.go).
// This file tells Phase 5 Slice 5's story end to end against the real
// composition root, config defaults included (BEECON_LOGIN_MAX_ATTEMPTS=5,
// BEECON_LOGIN_LOCKOUT=15m — test/support's testConfig sets neither field, so
// wiring.go's own loginMaxAttemptsOrDefault/loginLockoutOrDefault fall back to
// exactly the values a real unconfigured installation would run with):
// failing the default threshold locks the account -> the very next attempt,
// even with the correct password, is rejected 429 while still locked ->
// travelling the injected clock (no real sleep) past the cooldown window
// allows the correct password to succeed and mint a real session -> a
// separate fixture proves a successful login below the threshold resets the
// failed-attempt counter (one further wrong password afterward is a plain
// 401, not an immediate re-lock).
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"beecon/test/support"
)

func TestOperatorAuthLoginHardeningAndReauthJourney(t *testing.T) {
	const defaultMaxAttempts = 5
	const defaultLockout = 15 * time.Minute

	clock := support.NewMovableClock(time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, nil, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey

	bootstrap := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
		`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)
	if bootstrap.Code != http.StatusCreated {
		t.Fatalf("bootstrap fixture: status = %d, want %d; body=%s", bootstrap.Code, http.StatusCreated, bootstrap.Body.String())
	}

	wrongLogin := func(t *testing.T) *http.Response {
		t.Helper()
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"totally-wrong-password"}`, nil)
		return w.Result()
	}

	t.Run("failing the default max-attempts threshold locks the account", func(t *testing.T) {
		for i := 0; i < defaultMaxAttempts-1; i++ {
			resp := wrongLogin(t)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("attempt %d/%d: status = %d, want %d (not yet locked)", i+1, defaultMaxAttempts, resp.StatusCode, http.StatusUnauthorized)
			}
		}
		// The threshold-crossing attempt itself is still the generic 401
		// (architecture §5: the lock takes effect starting with the NEXT
		// request, never leaking mid-attempt that this guess tipped it over).
		resp := wrongLogin(t)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("the threshold-crossing attempt: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("the next attempt, even with the correct password, is rejected 429 while locked", func(t *testing.T) {
		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error body: %v; body=%s", err, w.Body.String())
		}
		if env.Error.Code != "account_locked" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "account_locked")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Error("expected no session cookies to be set while the account is locked")
		}
	})

	t.Run("still locked one second before the cooldown elapses", func(t *testing.T) {
		clock.Advance(defaultLockout - time.Second)

		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)

		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want %d — still inside the cooldown window; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
		}
	})

	t.Run("traveling the injected clock past the cooldown window allows the correct password to succeed, with no real sleep", func(t *testing.T) {
		clock.Advance(2 * time.Second) // now defaultLockout + 1s past the original lock

		w := doOperatorAuthRequest(t, wired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"founder@example.com","password":"correct horse battery staple"}`, nil)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		if len(w.Result().Cookies()) == 0 {
			t.Error("expected a real session to be minted once the cooldown has elapsed")
		}
	})

	// --- Separate fixture: a successful login below the threshold resets
	// the failed-attempt counter (crucial_path's own reset-on-success
	// story), proved against a second, freshly bootstrapped installation so
	// it never interacts with the lock/unlock timeline above. ---
	t.Run("a successful login below the threshold resets the failed-attempt counter", func(t *testing.T) {
		resetClock := support.NewMovableClock(time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC))
		resetWired := support.BootAppWithProviderDefinitionsAndClock(t, nil, resetClock.Now)
		bootstrapResp := doOperatorAuthRequest(t, resetWired.Router, http.MethodPost, "/api/v1/operators/bootstrap", adminAuth,
			`{"email":"reset-story@example.com","password":"correct horse battery staple"}`, nil)
		if bootstrapResp.Code != http.StatusCreated {
			t.Fatalf("bootstrap fixture: status = %d, want %d; body=%s", bootstrapResp.Code, http.StatusCreated, bootstrapResp.Body.String())
		}

		for i := 0; i < defaultMaxAttempts-1; i++ {
			w := doOperatorAuthRequest(t, resetWired.Router, http.MethodPost, "/api/v1/auth/login", "",
				`{"email":"reset-story@example.com","password":"totally-wrong-password"}`, nil)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("attempt %d/%d: status = %d, want %d", i+1, defaultMaxAttempts, w.Code, http.StatusUnauthorized)
			}
		}

		successW := doOperatorAuthRequest(t, resetWired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"reset-story@example.com","password":"correct horse battery staple"}`, nil)
		if successW.Code != http.StatusNoContent {
			t.Fatalf("the correct password one attempt below the threshold: status = %d, want %d; body=%s", successW.Code, http.StatusNoContent, successW.Body.String())
		}

		// If the counter had not been reset, this single further wrong
		// password (on top of the maxAttempts-1 already recorded) would cross
		// the threshold and lock the account instead of a plain 401.
		afterSuccessW := doOperatorAuthRequest(t, resetWired.Router, http.MethodPost, "/api/v1/auth/login", "",
			`{"email":"reset-story@example.com","password":"totally-wrong-password"}`, nil)
		if afterSuccessW.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d — a single wrong password right after a successful login must not immediately re-lock the account", afterSuccessW.Code, http.StatusUnauthorized)
		}
	})
}
