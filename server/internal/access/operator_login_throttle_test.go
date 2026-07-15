// Package access_test (see operator_facade_test.go's own header for why this
// is an external test package, and for the shared helpers reused below:
// newOperatorFacade, bootstrapOneOperator, sequentialTestIDs,
// testOperatorEmail/testOperatorPassword, operatorFacadeFixedTime). This file
// covers Phase 5 Slice 5's Login brute-force lockout (FD-G): a configurable
// per-account threshold and cooldown compared against the INJECTED clock
// (never wall-clock — the Slice 4 flaky-test lesson: a test that compares
// against time.Now() is a bug), a reset on a successful login, the
// per-account (not installation-wide) blast radius, and the "no existence
// leak" invariant — an unknown email is never given a lockout row and never
// escalates past the generic 401.
package access_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
)

// movableClockOperatorFacade builds an *access.OperatorFacade over fresh
// in-memory ports whose clock a test can advance without a real sleep — the
// same pattern operator_facade_test.go's own
// TestVerifySession_RejectsASessionPastItsAbsoluteExpiry uses, extracted here
// since every lockout test below needs to travel time past LockedUntil.
func movableClockOperatorFacade(t *testing.T, sessionTTL time.Duration) (*access.OperatorFacade, func(time.Duration)) {
	t.Helper()
	start, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	current := start
	clock := func() time.Time { return current }
	f := access.NewOperatorFacade(
		memory.NewOperatorRepository(),
		memory.NewOperatorSessionRepository(),
		sequentialTestIDs("op_"),
		sequentialTestIDs("opsess_"),
		clock,
		sessionTTL,
	)
	advance := func(d time.Duration) { current = current.Add(d) }
	return f, advance
}

const (
	loginThrottleTestMaxAttempts = 5
	loginThrottleTestLockout     = 15 * time.Minute
)

// failLoginNTimes runs n consecutive wrong-password Login attempts against
// email and asserts each one is rejected — it does not assert which error,
// since callers use it both below (and at) the lock threshold, where the
// expected error differs.
func loginWithWrongPassword(t *testing.T, f *access.OperatorFacade, email string) error {
	t.Helper()
	_, err := f.Login(context.Background(), email, "definitely-the-wrong-password")
	return err
}

// --- Threshold boundary (AC1: "after a configurable number of consecutive
// failed logins ... further attempts are locked"). ---

func TestLogin_EveryAttemptBelowTheThresholdReturnsTheGenericUnauthorizedNotALockout(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)

	for attempt := 1; attempt < loginThrottleTestMaxAttempts; attempt++ {
		err := loginWithWrongPassword(t, f, testOperatorEmail)
		de := assertDomainError(t, err, "unauthorized", 401)
		if de.Code == access.CodeAccountLocked {
			t.Fatalf("attempt %d/%d: got the lockout error before the threshold was reached", attempt, loginThrottleTestMaxAttempts)
		}
	}
}

func TestLogin_TheThresholdCrossingAttemptItselfStillReturnsGenericUnauthorized(t *testing.T) {
	// Pins the exact boundary semantics (architecture §5, Slice 5): the Nth
	// consecutive wrong password is the one that sets LockedUntil, but its OWN
	// response is still the generic 401 — the lock only takes effect starting
	// with the NEXT request, never leaking to the caller mid-attempt that this
	// particular guess was "the one that tipped it over".
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 1; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}

	err := loginWithWrongPassword(t, f, testOperatorEmail) // the Nth attempt

	de := assertDomainError(t, err, "unauthorized", 401)
	if de.Code == access.CodeAccountLocked {
		t.Fatal("the threshold-crossing attempt's own response must still be the generic invalid-credentials 401, not the lockout 429")
	}
}

func TestLogin_AfterTheThresholdIsCrossedTheNextWrongPasswordIsRejectedWithTheLockout429(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}

	err := loginWithWrongPassword(t, f, testOperatorEmail)

	assertDomainError(t, err, access.CodeAccountLocked, 429)
}

// TestLogin_ALockedAccountRejectsEvenTheCorrectPasswordWith429 is the
// crucial security assertion distinguishing a brute-force lockout from a
// plain wrong-password rejection: once locked, the password is never even
// examined — the CORRECT password is rejected exactly the same way a wrong
// one would be, until the cooldown elapses.
func TestLogin_ALockedAccountRejectsEvenTheCorrectPasswordWith429(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}

	_, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)

	assertDomainError(t, err, access.CodeAccountLocked, 429)
}

// --- Clock-travel unlock (AC1's cooldown window: injected clock, no real
// sleep — advancing past LockedUntil must restore login). ---

func TestLogin_AdvancingTheInjectedClockPastTheLockoutWindowAllowsTheCorrectPasswordToSucceedAgain(t *testing.T) {
	f, advance := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err == nil {
		t.Fatal("test fixture bug: expected the account to be locked before advancing the clock")
	}

	advance(loginThrottleTestLockout + time.Second) // no real sleep — the injected clock alone moves

	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)

	if err != nil {
		t.Fatalf("expected login to succeed once the injected clock has passed the lockout window, got: %v", err)
	}
	if session.Token == "" {
		t.Error("expected a real session to be minted once unlocked")
	}
}

func TestLogin_OneSecondBeforeTheLockoutWindowElapsesTheAccountIsStillLocked(t *testing.T) {
	f, advance := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}

	advance(loginThrottleTestLockout - time.Second)

	_, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)

	assertDomainError(t, err, access.CodeAccountLocked, 429)
}

// --- Reset on success (AC2). ---

func TestLogin_ASuccessfulLoginBelowTheThresholdResetsTheFailedAttemptCounterToZero(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts-1; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err != nil {
		t.Fatalf("the correct password one attempt below the threshold: unexpected error: %v", err)
	}

	// A single further wrong password must be the ordinary generic 401 — if
	// the counter had NOT been reset to zero by the successful login above,
	// this one attempt (on top of the maxAttempts-1 already recorded) would
	// cross the threshold and lock the account instead.
	err := loginWithWrongPassword(t, f, testOperatorEmail)

	de := assertDomainError(t, err, "unauthorized", 401)
	if de.Code == access.CodeAccountLocked {
		t.Fatal("a single wrong password right after a successful login must not immediately re-lock the account — the failed-attempt counter was not reset on success")
	}
}

// --- Per-account blast radius (AC3, first half). ---

func TestLogin_LockingOneOperatorsAccountDoesNotAffectAnotherOperatorsLogin(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f) // operator A: testOperatorEmail
	const secondOperatorEmail = "second-operator@example.com"
	const secondOperatorPassword = "another perfectly good password"
	if _, err := f.CreateOperator(context.Background(), secondOperatorEmail, secondOperatorPassword); err != nil {
		t.Fatalf("create second operator: unexpected error: %v", err)
	}
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err == nil {
		t.Fatal("test fixture bug: expected operator A to be locked")
	}

	_, err := f.Login(context.Background(), secondOperatorEmail, secondOperatorPassword)

	if err != nil {
		t.Fatalf("expected operator B's login to succeed while operator A is locked (per-account lockout), got: %v", err)
	}
}

// --- No existence leak / no unbounded row growth (AC3, second half). ---

func TestLogin_AnUnknownEmailNeverLocksRegardlessOfHowManyAttemptsAreMade(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	const unknownEmail = "nobody-has-this-account@example.com"

	// Deliberately more than loginThrottleTestMaxAttempts: an unknown email
	// must never accumulate toward a lockout, because it never resolves to an
	// operator row a lockout could be recorded against.
	for attempt := 0; attempt < loginThrottleTestMaxAttempts*2; attempt++ {
		err := loginWithWrongPassword(t, f, unknownEmail)
		de := assertDomainError(t, err, "unauthorized", 401)
		if de.Code == access.CodeAccountLocked {
			t.Fatalf("attempt %d against an unknown email produced the lockout error — an unknown email must never lock, or it becomes an existence oracle", attempt+1)
		}
	}
}

func TestLogin_AnUnknownEmailNeverCreatesAnOperatorRecordEvenAfterRepeatedAttempts(t *testing.T) {
	repository := memory.NewOperatorRepository()
	fixed, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	f := access.NewOperatorFacade(repository, memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), func() time.Time { return fixed }, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	const unknownEmail = "nobody-has-this-account@example.com"

	for attempt := 0; attempt < loginThrottleTestMaxAttempts*2; attempt++ {
		_ = loginWithWrongPassword(t, f, unknownEmail)
	}

	operators, err := repository.ListAll(context.Background())
	if err != nil {
		t.Fatalf("list operators: unexpected error: %v", err)
	}
	if len(operators) != 1 {
		t.Fatalf("got %d operator rows after repeatedly probing an unknown email, want exactly 1 (only the bootstrapped operator) — an unknown email must never grow a row", len(operators))
	}
	for _, operator := range operators {
		if operator.Email == unknownEmail {
			t.Fatal("an operator row exists for the unknown, never-registered email — this is both an existence leak and unbounded row growth from probing")
		}
	}
}

// --- The 429 body itself is as generic as the 401 (never leaks the email or
// any account-specific detail beyond "requests are throttled right now"). ---

func TestLogin_TheLockoutResponseMessageIsGenericAndNeverMentionsTheAccountsEmail(t *testing.T) {
	f, _ := movableClockOperatorFacade(t, time.Hour)
	f.WithLoginThrottle(loginThrottleTestMaxAttempts, loginThrottleTestLockout)
	bootstrapOneOperator(t, f)
	for attempt := 0; attempt < loginThrottleTestMaxAttempts; attempt++ {
		_ = loginWithWrongPassword(t, f, testOperatorEmail)
	}

	_, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)

	de := assertDomainError(t, err, access.CodeAccountLocked, 429)
	if de.Message != "too many failed attempts, try again later" {
		t.Errorf("message = %q, want the fixed generic lockout message", de.Message)
	}
	if de.Details != nil {
		t.Errorf("details = %+v, want nil — the 429 must carry no additional details that could narrow down the account", de.Details)
	}
}
