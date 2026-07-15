// Package access_test (see facade_test.go's own header for why this is an
// external test package). This file covers Phase 5 Slice 4's OperatorFacade
// additions: ListOperators (never a password hash), CreateOperator (an
// additional ACTIVE operator, distinct from Bootstrap), ChangeMyPassword
// (the critical AC4 keep-current-session semantics, closing the
// carried-forward Slice 2 AC4), Deactivate (plus the last-active-operator
// lock-out guard), and the break-glass ResetPassword (FD-B).
package access_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
)

// --- CreateOperator (AC1/AC2). ---

func TestCreateOperator_CreatesAnAdditionalActiveOperatorAccount(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	created, err := f.CreateOperator(context.Background(), "Second@Example.com", "another correct horse battery")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.Email != "second@example.com" {
		t.Errorf("email = %q, want lowercased %q", created.Email, "second@example.com")
	}
	if created.ID == "" {
		t.Fatal("expected a non-empty operator id")
	}
}

func TestCreateOperator_TheCreatedOperatorCanThenLogIn(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	created, err := f.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}

	session, err := f.Login(context.Background(), "second@example.com", "another correct horse battery")

	if err != nil {
		t.Fatalf("expected the newly created operator to log in, got: %v", err)
	}
	if session.OperatorID != created.ID {
		t.Errorf("session.OperatorID = %q, want %q", session.OperatorID, created.ID)
	}
}

func TestCreateOperator_RejectsADuplicateEmailCaseInsensitively(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f) // "operator@example.com"

	_, err := f.CreateOperator(context.Background(), "OPERATOR@EXAMPLE.COM", "another correct horse battery")

	assertDomainError(t, err, access.CodeEmailExists, 409)
}

func TestCreateOperator_RejectsAPasswordShorterThanTheMinimumLength(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	_, err := f.CreateOperator(context.Background(), "second@example.com", "short1")

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

func TestCreateOperator_DoesNotRequireAnyExistingOperatorFirst(t *testing.T) {
	// Distinct from Bootstrap: CreateOperator never checks "does any operator
	// exist" — it is gated only by email uniqueness. On a facade with no
	// operator at all yet, CreateOperator still succeeds (Bootstrap's own
	// first-account-only 409 does not apply here).
	f := newOperatorFacade(t, nil, time.Hour)

	_, err := f.CreateOperator(context.Background(), "first-ever@example.com", "another correct horse battery")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- ListOperators (AC3: email/status/created, never a password hash). ---

func TestListOperators_ReturnsEveryOperatorWithoutAPasswordHashField(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	created, err := f.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}

	summaries, err := f.ListOperators(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	byID := map[access.OperatorID]access.OperatorSummary{}
	for _, s := range summaries {
		byID[s.ID] = s
	}
	first, ok := byID[bootstrapped.ID]
	if !ok {
		t.Fatalf("expected the bootstrapped operator %q in the list", bootstrapped.ID)
	}
	if first.Email != "operator@example.com" || first.Status != access.OperatorStatusActive {
		t.Errorf("bootstrapped summary = %+v, want email operator@example.com, status ACTIVE", first)
	}
	if first.CreatedAt.IsZero() {
		t.Error("expected a non-zero CreatedAt")
	}
	second, ok := byID[created.ID]
	if !ok {
		t.Fatalf("expected the created operator %q in the list", created.ID)
	}
	if second.Email != "second@example.com" {
		t.Errorf("created summary email = %q, want %q", second.Email, "second@example.com")
	}
	// OperatorSummary's own type has no PasswordHash field at all — this
	// compiles only because that is true; there is nothing further to assert
	// at runtime beyond the field-level checks above (the type itself is the
	// guarantee, mirroring bootstrappedOperatorDTO's own shape).
}

// --- ChangeMyPassword (Slice 4 AC4; closes the carried-forward Slice 2 AC4):
// the critical keep-current-session semantics. ---

// threeSessionsForOneOperator logs the same operator in three times,
// returning the three independent LoggedInSessions in creation order — the
// fixture the keep-current test below needs (S1 = acting session, S2/S3 =
// the "other" sessions that must die).
func threeSessionsForOneOperator(t *testing.T, f *access.OperatorFacade, email, password string) (s1, s2, s3 access.LoggedInSession) {
	t.Helper()
	var err error
	s1, err = f.Login(context.Background(), email, password)
	if err != nil {
		t.Fatalf("login S1: unexpected error: %v", err)
	}
	s2, err = f.Login(context.Background(), email, password)
	if err != nil {
		t.Fatalf("login S2: unexpected error: %v", err)
	}
	s3, err = f.Login(context.Background(), email, password)
	if err != nil {
		t.Fatalf("login S3: unexpected error: %v", err)
	}
	return s1, s2, s3
}

// TestChangeMyPassword_RevokesOtherSessionsButKeepsTheActingSessionAlive is
// the direct AC4 assertion (the critical carry-forward from Slice 2): an
// operator with three live sessions (S1 acting, S2, S3) changes their own
// password identifying S1 as the current session — afterward S1 still
// verifies, S2 and S3 do not.
func TestChangeMyPassword_RevokesOtherSessionsButKeepsTheActingSessionAlive(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	s1, s2, s3 := threeSessionsForOneOperator(t, f, testOperatorEmail, testOperatorPassword)
	authenticatedS1, err := f.VerifySession(context.Background(), s1.Token)
	if err != nil {
		t.Fatalf("precondition: expected S1 to verify, got: %v", err)
	}

	err = f.ChangeMyPassword(context.Background(), bootstrapped.ID, authenticatedS1.SessionID, testOperatorPassword, "a brand new password entirely")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), s1.Token); err != nil {
		t.Errorf("expected the ACTING session S1 to remain valid after its own operator changed the password, got: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), s2.Token); err == nil {
		t.Error("expected session S2 to be revoked by ChangeMyPassword, but it still verifies")
	}
	if _, err := f.VerifySession(context.Background(), s3.Token); err == nil {
		t.Error("expected session S3 to be revoked by ChangeMyPassword, but it still verifies")
	}
}

func TestChangeMyPassword_TheNewPasswordThenLogsInAndTheOldOneNoLongerWorks(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	authenticated, err := f.VerifySession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("verify fixture: unexpected error: %v", err)
	}

	if err := f.ChangeMyPassword(context.Background(), bootstrapped.ID, authenticated.SessionID, testOperatorPassword, "a brand new password entirely"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := f.Login(context.Background(), testOperatorEmail, "a brand new password entirely"); err != nil {
		t.Errorf("expected login with the new password to succeed, got: %v", err)
	}
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err == nil {
		t.Error("expected login with the OLD password to be rejected once it has been changed")
	}
}

func TestChangeMyPassword_RejectsAWrongCurrentPasswordAndChangesNothing(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	s1, s2, _ := threeSessionsForOneOperator(t, f, testOperatorEmail, testOperatorPassword)
	authenticatedS1, err := f.VerifySession(context.Background(), s1.Token)
	if err != nil {
		t.Fatalf("precondition: expected S1 to verify, got: %v", err)
	}

	err = f.ChangeMyPassword(context.Background(), bootstrapped.ID, authenticatedS1.SessionID, "totally-wrong-current-password", "a brand new password entirely")

	assertDomainError(t, err, "unauthorized", 401)
	// Nothing changed: the OLD password still logs in...
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err != nil {
		t.Errorf("expected the old password to still work after a rejected change, got: %v", err)
	}
	// ...and neither S1 nor S2 was revoked by the rejected attempt.
	if _, err := f.VerifySession(context.Background(), s1.Token); err != nil {
		t.Errorf("expected S1 to remain valid after a rejected password change, got: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), s2.Token); err != nil {
		t.Errorf("expected S2 to remain valid (untouched) after a rejected password change, got: %v", err)
	}
}

func TestChangeMyPassword_RejectsANewPasswordShorterThanTheMinimumLength(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	authenticated, err := f.VerifySession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("verify fixture: unexpected error: %v", err)
	}

	err = f.ChangeMyPassword(context.Background(), bootstrapped.ID, authenticated.SessionID, testOperatorPassword, "short1")

	assertDomainError(t, err, access.CodeValidationFailed, 422)
	if _, err := f.VerifySession(context.Background(), session.Token); err != nil {
		t.Errorf("expected the session to remain valid after a rejected too-short new password, got: %v", err)
	}
}

// --- Deactivate (AC5) and the last-active-operator lock-out guard (AC6). ---

func TestDeactivate_DisablesTheTargetAndRevokesItsSessionsSoItCanNoLongerLogIn(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f) // operator A, kept ACTIVE (the guard needs a second active operator)
	target, err := f.CreateOperator(context.Background(), "target@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}
	targetSession, err := f.Login(context.Background(), "target@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}

	if err := f.Deactivate(context.Background(), target.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := f.Login(context.Background(), "target@example.com", "another correct horse battery"); err == nil {
		t.Error("expected a deactivated operator to no longer be able to log in")
	}
	if _, err := f.VerifySession(context.Background(), targetSession.Token); err == nil {
		t.Error("expected the deactivated operator's existing session to be invalidated immediately")
	}
}

func TestDeactivate_RejectsDeactivatingTheLastRemainingActiveOperator(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f) // the only active operator

	err := f.Deactivate(context.Background(), bootstrapped.ID)

	assertDomainError(t, err, access.CodeLastActiveOperator, 409)
}

func TestDeactivate_LastActiveGuard_NothingChangesWhenRejected(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}

	err = f.Deactivate(context.Background(), bootstrapped.ID)
	assertDomainError(t, err, access.CodeLastActiveOperator, 409)

	if _, err := f.VerifySession(context.Background(), session.Token); err != nil {
		t.Errorf("expected the rejected deactivation to leave the last operator's own session untouched, got: %v", err)
	}
	summaries, err := f.ListOperators(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Status != access.OperatorStatusActive {
		t.Errorf("expected the sole operator to remain ACTIVE and untouched, got %+v", summaries)
	}
}

// TestDeactivate_WithTwoActiveOperatorsOneCanBeDeactivated is the direct
// contrast with the last-active guard above: the guard only blocks the LAST
// one — with two ACTIVE operators, deactivating one succeeds outright.
func TestDeactivate_WithTwoActiveOperatorsOneCanBeDeactivated(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	second, err := f.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}

	if err := f.Deactivate(context.Background(), second.ID); err != nil {
		t.Fatalf("expected deactivating one of two active operators to succeed, got: %v", err)
	}

	summaries, err := f.ListOperators(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	activeCount := 0
	for _, s := range summaries {
		if s.Status == access.OperatorStatusActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Errorf("expected exactly 1 remaining ACTIVE operator, got %d", activeCount)
	}
}

func TestDeactivate_RejectsAnUnknownOperatorID(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)

	err := f.Deactivate(context.Background(), access.OperatorID("op_does-not-exist"))

	assertDomainError(t, err, access.CodeOperatorNotFound, 404)
}

// --- ResetPassword (FD-B, break-glass recovery). ---

func TestResetPassword_ReactivatesADisabledOperatorAndRevokesItsSessions(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	target, err := f.CreateOperator(context.Background(), "target@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}
	staleSession, err := f.Login(context.Background(), "target@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("login fixture: unexpected error: %v", err)
	}
	if err := f.Deactivate(context.Background(), target.ID); err != nil {
		t.Fatalf("deactivate fixture: unexpected error: %v", err)
	}

	if err := f.ResetPassword(context.Background(), target.ID, "a freshly reset password"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := f.VerifySession(context.Background(), staleSession.Token); err == nil {
		t.Error("expected the pre-reset session to remain revoked (ResetPassword must never resurrect an old session)")
	}
	if _, err := f.Login(context.Background(), "target@example.com", "a freshly reset password"); err != nil {
		t.Errorf("expected the reactivated operator to log in with the new password, got: %v", err)
	}
}

// TestResetPassword_WorksEvenWhenOperatorsAlreadyExist is the direct contrast
// with Bootstrap's own first-account-only 409 (FD-B's whole reason to
// exist): ResetPassword is not gated on OperatorsExist at all.
func TestResetPassword_WorksEvenWhenOperatorsAlreadyExist(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	second, err := f.CreateOperator(context.Background(), "second@example.com", "another correct horse battery")
	if err != nil {
		t.Fatalf("create fixture: unexpected error: %v", err)
	}

	if err := f.ResetPassword(context.Background(), second.ID, "a freshly reset password"); err != nil {
		t.Fatalf("expected ResetPassword to work even though %d operators already exist, got: %v", 2, err)
	}
	if _, err := f.Login(context.Background(), "second@example.com", "a freshly reset password"); err != nil {
		t.Errorf("expected the reset operator to log in with the new password, got: %v", err)
	}
	// bootstrapped's own account and password are entirely untouched.
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err != nil {
		t.Errorf("expected the OTHER (non-reset) operator %q to be unaffected, got: %v", bootstrapped.ID, err)
	}
}

// TestResetPassword_RecoversTheInstallationWhenEveryOperatorIsDisabled is the
// genuine "everyone is locked out" recovery scenario FD-B exists for: every
// operator account ends up DISABLED (Deactivate's own last-active guard would
// never let this happen through the facade itself — it can only arise from
// something outside ChangeMyPassword/Deactivate's reach, e.g. Slice 5's
// separate login-throttle mechanism or direct operational intervention, so
// this test drives the repository directly to reach that state, the same
// convention TestVerifySession_RejectsADisabledOperatorsSession already
// uses) — leaving nobody able to log in at all. The break-glass reset path
// still reaches and reactivates the targeted account regardless.
func TestResetPassword_RecoversTheInstallationWhenEveryOperatorIsDisabled(t *testing.T) {
	repository := memory.NewOperatorRepository()
	fixed, _ := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	f := access.NewOperatorFacade(repository, memory.NewOperatorSessionRepository(), sequentialTestIDs("op_"), sequentialTestIDs("opsess_"), func() time.Time { return fixed }, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)
	disabled, err := repository.FindByID(context.Background(), bootstrapped.ID)
	if err != nil || disabled == nil {
		t.Fatalf("find operator: %v", err)
	}
	disabled.Status = access.OperatorStatusDisabled
	if err := repository.Save(context.Background(), *disabled); err != nil {
		t.Fatalf("save disabled operator: %v", err)
	}
	if _, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword); err == nil {
		t.Fatal("precondition: expected the sole operator to be locked out (DISABLED) before recovery")
	}

	if err := f.ResetPassword(context.Background(), bootstrapped.ID, "recovery password here"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := f.Login(context.Background(), testOperatorEmail, "recovery password here"); err != nil {
		t.Errorf("expected the recovered (reactivated) operator to log in with the reset password, got: %v", err)
	}
}

func TestResetPassword_RejectsANewPasswordShorterThanTheMinimumLength(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapped := bootstrapOneOperator(t, f)

	err := f.ResetPassword(context.Background(), bootstrapped.ID, "short1")

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

func TestResetPassword_RejectsAnUnknownOperatorID(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	err := f.ResetPassword(context.Background(), access.OperatorID("op_does-not-exist"), "a perfectly fine password")

	assertDomainError(t, err, access.CodeOperatorNotFound, 404)
}
