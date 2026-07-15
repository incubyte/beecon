// Package access_test (see facade_test.go's own header for why this is an
// external test package). This file covers Phase 5 Slice 1's OperatorFacade
// (PD49/PD54/PD58): Bootstrap (first-account-only), Login (the generic
// wrong-password/unknown-email enumeration defense), VerifySession (expiry
// and disabled-operator rejection), and Me.
package access_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
)

const operatorFacadeFixedTime = "2026-07-15T09:00:00Z"

// newOperatorFacade wires an access.OperatorFacade over the in-memory ports
// with deterministic sequential ids and the given clock (fixed, unless a
// test needs to move it — see the VerifySession expiry test below), mirroring
// driven/memory's own sequentialIDs convention for the sibling access.Facade.
func newOperatorFacade(t *testing.T, now func() time.Time, sessionTTL time.Duration) *access.OperatorFacade {
	t.Helper()
	if now == nil {
		fixed, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
		if err != nil {
			t.Fatalf("parse fixed test time: %v", err)
		}
		now = func() time.Time { return fixed }
	}
	return access.NewOperatorFacade(
		memory.NewOperatorRepository(),
		memory.NewOperatorSessionRepository(),
		sequentialTestIDs("op_"),
		sequentialTestIDs("opsess_"),
		now,
		sessionTTL,
	)
}

func sequentialTestIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}

const (
	testOperatorEmail    = "Operator@Example.com"
	testOperatorPassword = "correct horse battery staple"
)

// --- Bootstrap (AC: first-account-only; validation; never returns the
// password/hash). ---

func TestBootstrap_CreatesTheFirstOperatorAccountWhenNoneExists(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	bootstrapped, err := f.Bootstrap(context.Background(), testOperatorEmail, testOperatorPassword)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bootstrapped.ID == "" {
		t.Fatal("expected a non-empty operator id")
	}
	if got, want := string(bootstrapped.ID)[:3], "op_"; got != want {
		t.Errorf("id prefix = %q, want %q (op_<cuid2> shape, PD58)", got, want)
	}
	if bootstrapped.Email != "operator@example.com" {
		t.Errorf("email = %q, want lowercased %q", bootstrapped.Email, "operator@example.com")
	}
}

func TestBootstrap_RejectsASecondBootstrapOnceAnOperatorAlreadyExists(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	if _, err := f.Bootstrap(context.Background(), testOperatorEmail, testOperatorPassword); err != nil {
		t.Fatalf("first bootstrap: unexpected error: %v", err)
	}

	_, err := f.Bootstrap(context.Background(), "second@example.com", testOperatorPassword)

	assertDomainError(t, err, access.CodeOperatorExists, 409)
}

func TestBootstrap_RejectsAPasswordShorterThanTheMinimumLengthNamingTheRequirement(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	_, err := f.Bootstrap(context.Background(), testOperatorEmail, "short1")

	de := assertDomainError(t, err, access.CodeValidationFailed, 422)
	if de.Details["field"] != "password" {
		t.Errorf("details.field = %v, want %q", de.Details["field"], "password")
	}
	issue, _ := de.Details["issue"].(string)
	if issue == "" {
		t.Fatal("expected details.issue to name the length requirement, got empty")
	}
}

func TestBootstrap_RejectsAMalformedEmail(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	_, err := f.Bootstrap(context.Background(), "not-an-email", testOperatorPassword)

	de := assertDomainError(t, err, access.CodeValidationFailed, 422)
	if de.Details["field"] != "email" {
		t.Errorf("details.field = %v, want %q", de.Details["field"], "email")
	}
}

func TestBootstrap_NeverPersistsThePlaintextPasswordOnTheStoredOperatorRecord(t *testing.T) {
	repository := memory.NewOperatorRepository()
	fixed, _ := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	f := access.NewOperatorFacade(repository, memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), func() time.Time { return fixed }, time.Hour)
	bootstrapped, err := f.Bootstrap(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := repository.FindByID(context.Background(), bootstrapped.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored == nil {
		t.Fatal("expected the bootstrapped operator to be persisted")
	}
	if stored.PasswordHash == testOperatorPassword {
		t.Fatal("stored PasswordHash equals the plaintext password — it must be an Argon2id hash")
	}
	if got, want := stored.PasswordHash[:10], "$argon2id$"; got != want {
		t.Errorf("stored PasswordHash = %q, want it to start with %q", stored.PasswordHash, want)
	}
}

// --- Login (AC: wrong password and unknown email are identically rejected;
// a disabled operator is rejected the same way; correct creds mint a
// session). ---

func bootstrapOneOperator(t *testing.T, f *access.OperatorFacade) access.BootstrappedOperator {
	t.Helper()
	bootstrapped, err := f.Bootstrap(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("bootstrap: unexpected error: %v", err)
	}
	return bootstrapped
}

func TestLogin_SucceedsWithTheCorrectEmailAndPasswordAndMintsASession(t *testing.T) {
	sessionTTL := time.Hour
	f := newOperatorFacade(t, nil, sessionTTL)
	bootstrapped := bootstrapOneOperator(t, f)
	fixedNow, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}

	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.OperatorID != bootstrapped.ID {
		t.Errorf("session.OperatorID = %q, want %q", session.OperatorID, bootstrapped.ID)
	}
	if session.Token == "" {
		t.Error("expected a non-empty opaque session token")
	}
	if session.CSRFToken == "" {
		t.Error("expected a non-empty CSRF token")
	}
	if session.CSRFToken == session.Token {
		t.Error("CSRFToken and Token must be independently generated (PD52), not the same value")
	}
	// Deterministic against the facade's INJECTED clock (never real
	// time.Now() — the fixed fixture instant can otherwise fall behind the
	// sandbox's real wall-clock and make this test flaky).
	wantExpiresAt := fixedNow.Add(sessionTTL)
	if !session.ExpiresAt.Equal(wantExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v (fixed clock %v + session TTL %v)", session.ExpiresAt, wantExpiresAt, fixedNow, sessionTTL)
	}
}

func TestLogin_IsCaseInsensitiveOnEmail(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f) // stored as "operator@example.com" (bootstrapped with mixed case)

	_, err := f.Login(context.Background(), "OPERATOR@EXAMPLE.COM", testOperatorPassword)

	if err != nil {
		t.Fatalf("expected login to succeed case-insensitively on email, got: %v", err)
	}
}

func TestLogin_RejectsAWrongPasswordWithTheGenericInvalidCredentialsError(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	_, err := f.Login(context.Background(), testOperatorEmail, "wrong-password-entirely")

	assertDomainError(t, err, "unauthorized", 401)
	if err.Error() != "invalid credentials" {
		t.Errorf("error message = %q, want the generic %q", err.Error(), "invalid credentials")
	}
}

func TestLogin_RejectsAnUnknownEmailWithTheIdenticalGenericInvalidCredentialsError(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	_, err := f.Login(context.Background(), "nobody-has-this-account@example.com", testOperatorPassword)

	assertDomainError(t, err, "unauthorized", 401)
	if err.Error() != "invalid credentials" {
		t.Errorf("error message = %q, want the generic %q", err.Error(), "invalid credentials")
	}
}

// TestLogin_WrongPasswordAndUnknownEmailProduceByteIdenticalErrors is the
// direct AC assertion (PD49: "does not reveal which was wrong"): both
// rejection paths must render literally the same status, code, and message —
// a client (or an attacker probing the endpoint) cannot distinguish "this
// email exists but the password was wrong" from "no such account".
func TestLogin_WrongPasswordAndUnknownEmailProduceByteIdenticalErrors(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	_, wrongPasswordErr := f.Login(context.Background(), testOperatorEmail, "wrong-password-entirely")
	_, unknownEmailErr := f.Login(context.Background(), "nobody-has-this-account@example.com", testOperatorPassword)

	wrongPasswordDE := assertDomainError(t, wrongPasswordErr, "unauthorized", 401)
	unknownEmailDE := assertDomainError(t, unknownEmailErr, "unauthorized", 401)
	if wrongPasswordDE.Message != unknownEmailDE.Message {
		t.Errorf("wrong-password message %q != unknown-email message %q — the response leaks which case occurred", wrongPasswordDE.Message, unknownEmailDE.Message)
	}
}

func TestLogin_RejectsAnEmptyPasswordTheSameGenericWay(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	_, err := f.Login(context.Background(), testOperatorEmail, "")

	assertDomainError(t, err, "unauthorized", 401)
}

// --- VerifySession (AC: valid session authenticates; expired/revoked/
// disabled-operator sessions do not). ---

func TestVerifySession_SucceedsForAFreshlyMintedSession(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}

	authenticated, err := f.VerifySession(context.Background(), session.Token)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authenticated.OperatorID != bootstrapped.ID {
		t.Errorf("OperatorID = %q, want %q", authenticated.OperatorID, bootstrapped.ID)
	}
	if authenticated.CSRFToken != session.CSRFToken {
		t.Errorf("CSRFToken = %q, want the session's own %q", authenticated.CSRFToken, session.CSRFToken)
	}
}

func TestVerifySession_RejectsAnUnknownToken(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	_, err := f.VerifySession(context.Background(), "a-token-that-was-never-issued")

	assertDomainError(t, err, "unauthorized", 401)
}

// TestVerifySession_RejectsASessionPastItsAbsoluteExpiry drives the clock
// forward past BEECON_SESSION_TTL (an injected movable clock, not a real
// sleep) and asserts the same session token no longer authenticates.
func TestVerifySession_RejectsASessionPastItsAbsoluteExpiry(t *testing.T) {
	start, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	current := start
	clock := func() time.Time { return current }
	sessionTTL := time.Hour
	f := access.NewOperatorFacade(memory.NewOperatorRepository(), memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), clock, sessionTTL)
	bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}

	current = start.Add(sessionTTL + time.Second)

	_, err = f.VerifySession(context.Background(), session.Token)

	assertDomainError(t, err, "unauthorized", 401)
}

func TestVerifySession_SucceedsOneSecondBeforeExpiry(t *testing.T) {
	start, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	current := start
	clock := func() time.Time { return current }
	sessionTTL := time.Hour
	f := access.NewOperatorFacade(memory.NewOperatorRepository(), memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), clock, sessionTTL)
	bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}

	current = start.Add(sessionTTL - time.Second)

	if _, err := f.VerifySession(context.Background(), session.Token); err != nil {
		t.Fatalf("expected a session one second before its absolute expiry to still verify, got: %v", err)
	}
}

// TestVerifySession_RejectsADisabledOperatorsSession pins the port-level
// building block Slice 2/4's Deactivate will rely on: VerifySession itself
// already re-checks the operator's current Status on every call (not just at
// login time), so disabling an operator immediately invalidates any session
// it already minted — the memory.OperatorRepository fake stands in for
// "Deactivate flipped the row" since Deactivate itself doesn't exist until
// Slice 4.
func TestVerifySession_RejectsADisabledOperatorsSession(t *testing.T) {
	repository := memory.NewOperatorRepository()
	fixed, _ := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	f := access.NewOperatorFacade(repository, memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), func() time.Time { return fixed }, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	disabled, err := repository.FindByID(context.Background(), bootstrapped.ID)
	if err != nil || disabled == nil {
		t.Fatalf("find operator: %v", err)
	}
	disabled.Status = access.OperatorStatusDisabled
	if err := repository.Save(context.Background(), *disabled); err != nil {
		t.Fatalf("save disabled operator: %v", err)
	}

	_, err = f.VerifySession(context.Background(), session.Token)

	assertDomainError(t, err, "unauthorized", 401)
}

// --- Me (AC: never returns a password hash). ---

func TestMe_ReturnsTheAuthenticatedOperatorsOwnIdentity(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)

	profile, err := f.Me(context.Background(), bootstrapped.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if profile.ID != bootstrapped.ID {
		t.Errorf("ID = %q, want %q", profile.ID, bootstrapped.ID)
	}
	if profile.Email != bootstrapped.Email {
		t.Errorf("Email = %q, want %q", profile.Email, bootstrapped.Email)
	}
}

func TestMe_RejectsAnUnknownOperatorID(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	_, err := f.Me(context.Background(), access.OperatorID("op_does-not-exist"))

	assertDomainError(t, err, "unauthorized", 401)
}

// --- OperatorsExist (the predicate authmw.ConsoleAuth's admin-key demotion
// branch reads — Slice 4's AC8, but the predicate itself exists from Slice
// 1). ---

func TestOperatorsExist_FalseBeforeAnyBootstrap(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	exists, err := f.OperatorsExist(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected OperatorsExist to be false before Bootstrap has ever run")
	}
}

func TestOperatorsExist_TrueAfterBootstrap(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	exists, err := f.OperatorsExist(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected OperatorsExist to be true once Bootstrap has succeeded")
	}
}
