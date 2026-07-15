// Package access_test — see operator_facade_test.go's own header for the
// module this covers. This file is Phase 5 Slice 2 (PD51/PD52): Logout
// (server-side revocation + idempotency), replay protection on an already
// revoked/expired session, and RevokeAllForOperator — the port-level
// mechanism Slice 4's password-change and deactivate paths will call, wired
// and testable now even though neither of those facade methods exists yet.
//
// Helpers reused from operator_facade_test.go (same package): newOperatorFacade,
// bootstrapOneOperator, sequentialTestIDs, assertDomainError,
// operatorFacadeFixedTime, testOperatorEmail/testOperatorPassword.
package access_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
)

// --- Logout (AC1, AC7: revokes server-side; idempotent). ---

// TestLogout_RevokesTheSessionServerSideSoTheSameCookieTokenFailsAfterward is
// the crucial security assertion: a copied/replayed cookie value must not
// keep working after logout, so this checks VerifySession itself rejects the
// exact same token post-logout — not merely that the handler would clear a
// cookie (a cookie clear alone would not stop a copied token).
func TestLogout_RevokesTheSessionServerSideSoTheSameCookieTokenFailsAfterward(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), session.Token); err != nil {
		t.Fatalf("precondition: expected the freshly minted session to verify, got: %v", err)
	}

	if err := f.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("logout: unexpected error: %v", err)
	}

	_, err = f.VerifySession(context.Background(), session.Token)
	assertDomainError(t, err, "unauthorized", 401)
}

func TestLogout_IsIdempotentOnASecondCallWithTheSameNowRevokedToken(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	if err := f.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("first logout: unexpected error: %v", err)
	}

	err = f.Logout(context.Background(), session.Token)

	if err != nil {
		t.Fatalf("second logout of an already-revoked token: expected no error (idempotent), got: %v", err)
	}
}

func TestLogout_IsANoOpForATokenThatWasNeverIssued(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	err := f.Logout(context.Background(), "a-token-that-was-never-issued")

	if err != nil {
		t.Fatalf("expected no error logging out an unknown token, got: %v", err)
	}
}

// TestLogout_IsANoOpForAnEmptyToken pins the handler-level contract from the
// facade side: OperatorHandler.Logout only calls the facade at all when a
// cookie is present, but the facade itself must not error on an empty/absent
// token either, so a caller can never turn "no session" into a 500.
func TestLogout_IsANoOpForAnEmptyToken(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)

	err := f.Logout(context.Background(), "")

	if err != nil {
		t.Fatalf("expected no error logging out with an empty token, got: %v", err)
	}
}

// --- Replay protection (AC6: a revoked session is never resurrected). ---

func TestVerifySession_NeverResurrectsARevokedSessionAcrossRepeatedReplayAttempts(t *testing.T) {
	f := newOperatorFacade(t, nil, time.Hour)
	bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	if err := f.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("logout: unexpected error: %v", err)
	}

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := f.VerifySession(context.Background(), session.Token); err == nil {
			t.Fatalf("replay attempt %d: expected the revoked session to stay rejected, got no error", attempt)
		}
	}
}

// --- RevokeAllForOperator (Slice 4's password-change/deactivate mechanism —
// wired at the port level in Slice 2 so it is testable now). ---

// newOperatorFacadeKeepingSessionsRepo mirrors newOperatorFacade but also
// hands back the underlying memory.OperatorSessionRepository, so a test can
// call RevokeAllForOperator directly (no facade method reaches it until
// Slice 4's ChangeMyPassword/Deactivate exist).
func newOperatorFacadeKeepingSessionsRepo(t *testing.T) (*access.OperatorFacade, *memory.OperatorSessionRepository) {
	t.Helper()
	fixed, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	sessionsRepo := memory.NewOperatorSessionRepository()
	f := access.NewOperatorFacade(
		memory.NewOperatorRepository(),
		sessionsRepo,
		sequentialTestIDs("op_"),
		sequentialTestIDs("opsess_"),
		func() time.Time { return fixed },
		time.Hour,
	)
	return f, sessionsRepo
}

func TestRevokeAllForOperator_InvalidatesEveryActiveSessionMintedForThatOperator(t *testing.T) {
	f, sessionsRepo := newOperatorFacadeKeepingSessionsRepo(t)
	bootstrapped := bootstrapOneOperator(t, f)
	// Login twice for the same operator — mints two independent sessions,
	// the way two browser tabs/devices would.
	sessionA, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login (A): unexpected error: %v", err)
	}
	sessionB, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login (B): unexpected error: %v", err)
	}

	if err := sessionsRepo.RevokeAllForOperator(context.Background(), bootstrapped.ID, time.Now()); err != nil {
		t.Fatalf("RevokeAllForOperator: unexpected error: %v", err)
	}

	if _, err := f.VerifySession(context.Background(), sessionA.Token); err == nil {
		t.Error("expected session A to be rejected after RevokeAllForOperator")
	}
	if _, err := f.VerifySession(context.Background(), sessionB.Token); err == nil {
		t.Error("expected session B to be rejected after RevokeAllForOperator")
	}
}

// TestRevokeAllForOperator_DoesNotAffectAnotherOperatorsSession models a
// second operator's session directly at the port level: Bootstrap is
// first-account-only (Slice 4's CreateOperator does not exist yet), so a
// genuinely distinct second operator id can only be modeled against the
// port both this slice and Slice 4 share.
func TestRevokeAllForOperator_DoesNotAffectAnotherOperatorsSession(t *testing.T) {
	f, sessionsRepo := newOperatorFacadeKeepingSessionsRepo(t)
	bootstrapped := bootstrapOneOperator(t, f)
	targetSession, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	otherSession := access.OperatorSession{
		ID:         "opsess_other",
		OperatorID: "op_other",
		TokenHash:  []byte("other-operators-session-token-hash"),
		CSRFToken:  "csrf-other",
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}
	if err := sessionsRepo.Save(context.Background(), otherSession); err != nil {
		t.Fatalf("save other operator's session: unexpected error: %v", err)
	}

	if err := sessionsRepo.RevokeAllForOperator(context.Background(), bootstrapped.ID, time.Now()); err != nil {
		t.Fatalf("RevokeAllForOperator: unexpected error: %v", err)
	}

	if _, err := f.VerifySession(context.Background(), targetSession.Token); err == nil {
		t.Error("expected the bootstrapped operator's own session to be revoked")
	}
	got, err := sessionsRepo.FindByTokenHash(context.Background(), otherSession.TokenHash)
	if err != nil {
		t.Fatalf("find other operator's session: unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected the other operator's session to still exist")
	}
	if got.RevokedAt != nil {
		t.Errorf("expected another operator's session to be untouched by RevokeAllForOperator, RevokedAt = %v", got.RevokedAt)
	}
}

func TestRevokeAllForOperator_IsIdempotentOnASecondCall(t *testing.T) {
	f, sessionsRepo := newOperatorFacadeKeepingSessionsRepo(t)
	bootstrapped := bootstrapOneOperator(t, f)
	session, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	if err := sessionsRepo.RevokeAllForOperator(context.Background(), bootstrapped.ID, time.Now()); err != nil {
		t.Fatalf("first RevokeAllForOperator: unexpected error: %v", err)
	}

	err = sessionsRepo.RevokeAllForOperator(context.Background(), bootstrapped.ID, time.Now().Add(time.Minute))

	if err != nil {
		t.Fatalf("second RevokeAllForOperator call: expected no error (idempotent), got: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), session.Token); err == nil {
		t.Error("expected the session to remain revoked after a second RevokeAllForOperator call")
	}
}

// --- crucial_path (Slice 2 end-to-end): login -> authed request OK ->
// logout -> same cookie -> unauthorized -> login again -> clock travels past
// TTL -> unauthorized. ---

func TestOperatorSessionLifecycle_LoginLogoutReplayLoginThenExpiryPastTTL(t *testing.T) {
	start, err := time.Parse(time.RFC3339, operatorFacadeFixedTime)
	if err != nil {
		t.Fatalf("parse fixed test time: %v", err)
	}
	current := start
	clock := func() time.Time { return current }
	sessionTTL := time.Hour
	f := access.NewOperatorFacade(
		memory.NewOperatorRepository(),
		memory.NewOperatorSessionRepository(),
		sequentialTestIDs("op_"),
		sequentialTestIDs("opsess_"),
		clock,
		sessionTTL,
	)
	bootstrapOneOperator(t, f)

	// login -> authed request OK
	firstSession, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("first login: unexpected error: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), firstSession.Token); err != nil {
		t.Fatalf("expected the freshly minted session to authenticate, got: %v", err)
	}

	// logout -> same cookie -> unauthorized
	if err := f.Logout(context.Background(), firstSession.Token); err != nil {
		t.Fatalf("logout: unexpected error: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), firstSession.Token); err == nil {
		t.Fatal("expected the logged-out session's cookie to be rejected")
	}

	// login again
	secondSession, err := f.Login(context.Background(), testOperatorEmail, testOperatorPassword)
	if err != nil {
		t.Fatalf("second login: unexpected error: %v", err)
	}
	if _, err := f.VerifySession(context.Background(), secondSession.Token); err != nil {
		t.Fatalf("expected the freshly re-logged-in session to authenticate, got: %v", err)
	}

	// clock travels past TTL (no real sleep) -> unauthorized
	current = start.Add(sessionTTL * 2)
	if _, err := f.VerifySession(context.Background(), secondSession.Token); err == nil {
		t.Fatal("expected the session to be rejected once the clock has passed the absolute TTL")
	}
}
