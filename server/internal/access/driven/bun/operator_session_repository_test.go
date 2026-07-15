// Package bun_test — see operator_repository_test.go's own header for the
// real-SQLite rationale. This file covers migration 0021's operator_sessions
// table: TokenHash round-trips through its hex-encoded storage column, and
// the UNIQUE INDEX on token_hash is a real SQL-level guarantee.
package bun_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/access"
	accessbun "beecon/internal/access/driven/bun"
	"beecon/internal/db"
)

var operatorSessionRepoTestDSNCounter int64

func newTestOperatorSessionRepository(t *testing.T) *accessbun.OperatorSessionRepository {
	t.Helper()
	n := atomic.AddInt64(&operatorSessionRepoTestDSNCounter, 1)
	dsn := fmt.Sprintf("file:operator_session_repo_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return accessbun.NewOperatorSessionRepository(database)
}

func testSession(id access.OperatorSessionID, token string) access.OperatorSession {
	return testSessionForOperator(id, "op_1", token)
}

// testSessionForOperator is testSession with an explicit operator id, for
// Slice 2's RevokeAllForOperator tests, which need more than one operator id
// in play.
func testSessionForOperator(id access.OperatorSessionID, operatorID access.OperatorID, token string) access.OperatorSession {
	hash := sha256.Sum256([]byte(token))
	return access.OperatorSession{
		ID:         id,
		OperatorID: operatorID,
		TokenHash:  hash[:],
		CSRFToken:  "csrf-token-value",
		CreatedAt:  time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, 7, 15, 21, 0, 0, 0, time.UTC),
	}
}

func TestOperatorSessionRepository_SaveThenFindByTokenHash_RoundTripsEveryField(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	session := testSession("opsess_1", "the-opaque-session-token")

	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.FindByTokenHash(context.Background(), session.TokenHash)
	if err != nil {
		t.Fatalf("find by token hash: %v", err)
	}
	if got == nil {
		t.Fatal("expected to find the saved session, got nil")
	}
	if got.ID != session.ID || got.OperatorID != session.OperatorID || got.CSRFToken != session.CSRFToken {
		t.Errorf("FindByTokenHash = %+v, want %+v", *got, session)
	}
	if !bytes.Equal(got.TokenHash, session.TokenHash) {
		t.Errorf("TokenHash = %x, want %x", got.TokenHash, session.TokenHash)
	}
	if !got.CreatedAt.Equal(session.CreatedAt) || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Errorf("CreatedAt/ExpiresAt = %v/%v, want %v/%v", got.CreatedAt, got.ExpiresAt, session.CreatedAt, session.ExpiresAt)
	}
	if got.RevokedAt != nil {
		t.Errorf("RevokedAt = %v, want nil for a freshly minted session", got.RevokedAt)
	}
}

func TestOperatorSessionRepository_FindByTokenHash_ReturnsNilNilForAnUnknownHash(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	unknownHash := sha256.Sum256([]byte("never-issued"))

	got, err := repo.FindByTokenHash(context.Background(), unknownHash[:])

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for an unknown token hash, got %+v", got)
	}
}

func TestOperatorSessionRepository_FindByTokenHash_DoesNotMatchADifferentSessionsHash(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	if err := repo.Save(context.Background(), testSession("opsess_1", "session-one-token")); err != nil {
		t.Fatalf("save: %v", err)
	}
	otherHash := sha256.Sum256([]byte("session-two-token"))

	got, err := repo.FindByTokenHash(context.Background(), otherHash[:])

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for a hash that was never saved, got session %q", got.ID)
	}
}

// TestOperatorSessionRepository_Save_RejectsADuplicateTokenHashViaTheUniqueIndex
// pins migration 0021's `UNIQUE INDEX idx_operator_sessions_token_hash`: two
// sessions can never collide on the same stored hash (which would otherwise
// let FindByTokenHash resolve to the wrong session).
func TestOperatorSessionRepository_Save_RejectsADuplicateTokenHashViaTheUniqueIndex(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	first := testSession("opsess_1", "shared-token-value")
	if err := repo.Save(context.Background(), first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	second := testSession("opsess_2", "shared-token-value") // same token -> same TokenHash

	err := repo.Save(context.Background(), second)

	if err == nil {
		t.Fatal("expected the second Save with a duplicate token_hash to fail the unique index constraint, got nil error")
	}
}

// --- Revoke / RevokeAllForOperator (Slice 2, PD51: Logout, and the port-level
// mechanism Slice 4's password-change/deactivate paths will call). ---

// TestOperatorSessionRepository_Revoke_MarksRevokedAtButFindByTokenHashStillReturnsTheRow
// pins that revocation is enforced by the caller (VerifySession checking
// IsRevoked), not by the repository hiding the row: a revoked session's row
// must still be findable by its hash so VerifySession can inspect and reject
// it — a query that silently excluded revoked rows would make this
// impossible to distinguish from "unknown token" at the facade level.
func TestOperatorSessionRepository_Revoke_MarksRevokedAtButFindByTokenHashStillReturnsTheRow(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	session := testSession("opsess_1", "the-opaque-session-token")
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("save: %v", err)
	}
	revokedAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	if err := repo.Revoke(context.Background(), session.ID, revokedAt); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := repo.FindByTokenHash(context.Background(), session.TokenHash)
	if err != nil {
		t.Fatalf("find by token hash: %v", err)
	}
	if got == nil {
		t.Fatal("expected FindByTokenHash to still return the revoked row")
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(revokedAt) {
		t.Errorf("RevokedAt = %v, want %v", got.RevokedAt, revokedAt)
	}
}

func TestOperatorSessionRepository_Revoke_IsANoOpForAnUnknownSessionID(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)

	err := repo.Revoke(context.Background(), "opsess_does-not-exist", time.Now())

	if err != nil {
		t.Fatalf("expected revoking an unknown session id to be a no-op, got: %v", err)
	}
}

// TestOperatorSessionRepository_Revoke_IsIdempotentAndDoesNotOverwriteTheFirstRevocationInstant
// pins the "WHERE revoked_at IS NULL" clause: a second Revoke call against an
// already-revoked session must not clobber the original revocation instant.
func TestOperatorSessionRepository_Revoke_IsIdempotentAndDoesNotOverwriteTheFirstRevocationInstant(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	session := testSession("opsess_1", "the-opaque-session-token")
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("save: %v", err)
	}
	firstRevoke := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	secondRevoke := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)
	if err := repo.Revoke(context.Background(), session.ID, firstRevoke); err != nil {
		t.Fatalf("first revoke: %v", err)
	}

	err := repo.Revoke(context.Background(), session.ID, secondRevoke)

	if err != nil {
		t.Fatalf("second revoke: expected no error (idempotent), got: %v", err)
	}
	got, err := repo.FindByTokenHash(context.Background(), session.TokenHash)
	if err != nil {
		t.Fatalf("find by token hash: %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(firstRevoke) {
		t.Errorf("RevokedAt = %v, want it to stay at the first revocation instant %v", got.RevokedAt, firstRevoke)
	}
}

// TestOperatorSessionRepository_RevokeAllForOperator_RevokesEveryActiveSessionForThatOperator
// is the real-SQLite version of the mechanism Slice 4's password-change and
// deactivate paths ride on: two sessions for one operator, one call, both
// rejected afterward.
func TestOperatorSessionRepository_RevokeAllForOperator_RevokesEveryActiveSessionForThatOperator(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	sessionA := testSessionForOperator("opsess_a", "op_shared", "token-a")
	sessionB := testSessionForOperator("opsess_b", "op_shared", "token-b")
	if err := repo.Save(context.Background(), sessionA); err != nil {
		t.Fatalf("save session A: %v", err)
	}
	if err := repo.Save(context.Background(), sessionB); err != nil {
		t.Fatalf("save session B: %v", err)
	}
	revokedAt := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	if err := repo.RevokeAllForOperator(context.Background(), "op_shared", revokedAt); err != nil {
		t.Fatalf("RevokeAllForOperator: %v", err)
	}

	gotA, err := repo.FindByTokenHash(context.Background(), sessionA.TokenHash)
	if err != nil {
		t.Fatalf("find session A: %v", err)
	}
	gotB, err := repo.FindByTokenHash(context.Background(), sessionB.TokenHash)
	if err != nil {
		t.Fatalf("find session B: %v", err)
	}
	if gotA == nil || gotA.RevokedAt == nil || !gotA.RevokedAt.Equal(revokedAt) {
		t.Errorf("session A RevokedAt = %+v, want %v", gotA, revokedAt)
	}
	if gotB == nil || gotB.RevokedAt == nil || !gotB.RevokedAt.Equal(revokedAt) {
		t.Errorf("session B RevokedAt = %+v, want %v", gotB, revokedAt)
	}
}

func TestOperatorSessionRepository_RevokeAllForOperator_DoesNotAffectAnotherOperatorsSession(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	targetSession := testSessionForOperator("opsess_target", "op_target", "target-token")
	otherSession := testSessionForOperator("opsess_other", "op_other", "other-token")
	if err := repo.Save(context.Background(), targetSession); err != nil {
		t.Fatalf("save target session: %v", err)
	}
	if err := repo.Save(context.Background(), otherSession); err != nil {
		t.Fatalf("save other session: %v", err)
	}

	if err := repo.RevokeAllForOperator(context.Background(), "op_target", time.Now()); err != nil {
		t.Fatalf("RevokeAllForOperator: %v", err)
	}

	gotOther, err := repo.FindByTokenHash(context.Background(), otherSession.TokenHash)
	if err != nil {
		t.Fatalf("find other session: %v", err)
	}
	if gotOther == nil {
		t.Fatal("expected the other operator's session to still exist")
	}
	if gotOther.RevokedAt != nil {
		t.Errorf("expected another operator's session to be untouched, RevokedAt = %v", gotOther.RevokedAt)
	}
}

func TestOperatorSessionRepository_RevokeAllForOperator_IsIdempotentOnASecondCall(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	session := testSessionForOperator("opsess_1", "op_shared", "token-1")
	if err := repo.Save(context.Background(), session); err != nil {
		t.Fatalf("save: %v", err)
	}
	firstRevoke := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	secondRevoke := time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC)
	if err := repo.RevokeAllForOperator(context.Background(), "op_shared", firstRevoke); err != nil {
		t.Fatalf("first RevokeAllForOperator: %v", err)
	}

	err := repo.RevokeAllForOperator(context.Background(), "op_shared", secondRevoke)

	if err != nil {
		t.Fatalf("second RevokeAllForOperator call: expected no error, got: %v", err)
	}
	got, err := repo.FindByTokenHash(context.Background(), session.TokenHash)
	if err != nil {
		t.Fatalf("find by token hash: %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(firstRevoke) {
		t.Errorf("RevokedAt = %v, want it to stay at the first call's instant %v", got.RevokedAt, firstRevoke)
	}
}

func TestOperatorSessionRepository_RevokeAllForOperator_IsANoOpForAnOperatorWithNoSessions(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)

	err := repo.RevokeAllForOperator(context.Background(), "op_has-no-sessions", time.Now())

	if err != nil {
		t.Fatalf("expected no error revoking all sessions for an operator with none, got: %v", err)
	}
}

// --- RevokeAllForOperatorExcept (Slice 4's ChangeMyPassword keep-current
// mechanism, carried forward from Slice 2 AC4 — the critical AC at the real-
// SQLite level). ---

// TestOperatorSessionRepository_RevokeAllForOperatorExcept_KeepsTheExceptedSessionAliveAndRevokesTheRest
// is the direct real-SQLite pin of the AC4 mechanism: three sessions for one
// operator, one call naming one of them as the exception — that one row's
// revoked_at must stay NULL while the other two are set.
func TestOperatorSessionRepository_RevokeAllForOperatorExcept_KeepsTheExceptedSessionAliveAndRevokesTheRest(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	acting := testSessionForOperator("opsess_acting", "op_shared", "acting-token")
	other1 := testSessionForOperator("opsess_other1", "op_shared", "other-token-1")
	other2 := testSessionForOperator("opsess_other2", "op_shared", "other-token-2")
	for _, s := range []access.OperatorSession{acting, other1, other2} {
		if err := repo.Save(context.Background(), s); err != nil {
			t.Fatalf("save %s: %v", s.ID, err)
		}
	}
	revokedAt := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)

	if err := repo.RevokeAllForOperatorExcept(context.Background(), "op_shared", "opsess_acting", revokedAt); err != nil {
		t.Fatalf("RevokeAllForOperatorExcept: %v", err)
	}

	gotActing, err := repo.FindByTokenHash(context.Background(), acting.TokenHash)
	if err != nil {
		t.Fatalf("find acting session: %v", err)
	}
	if gotActing == nil || gotActing.RevokedAt != nil {
		t.Errorf("acting session RevokedAt = %+v, want nil (the acting session must survive)", gotActing)
	}
	gotOther1, err := repo.FindByTokenHash(context.Background(), other1.TokenHash)
	if err != nil {
		t.Fatalf("find other1 session: %v", err)
	}
	if gotOther1 == nil || gotOther1.RevokedAt == nil || !gotOther1.RevokedAt.Equal(revokedAt) {
		t.Errorf("other1 session RevokedAt = %+v, want %v", gotOther1, revokedAt)
	}
	gotOther2, err := repo.FindByTokenHash(context.Background(), other2.TokenHash)
	if err != nil {
		t.Fatalf("find other2 session: %v", err)
	}
	if gotOther2 == nil || gotOther2.RevokedAt == nil || !gotOther2.RevokedAt.Equal(revokedAt) {
		t.Errorf("other2 session RevokedAt = %+v, want %v", gotOther2, revokedAt)
	}
}

func TestOperatorSessionRepository_RevokeAllForOperatorExcept_DoesNotAffectAnotherOperatorsSession(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	acting := testSessionForOperator("opsess_acting", "op_target", "acting-token")
	otherOperator := testSessionForOperator("opsess_other_op", "op_different", "other-operator-token")
	if err := repo.Save(context.Background(), acting); err != nil {
		t.Fatalf("save acting: %v", err)
	}
	if err := repo.Save(context.Background(), otherOperator); err != nil {
		t.Fatalf("save otherOperator: %v", err)
	}

	if err := repo.RevokeAllForOperatorExcept(context.Background(), "op_target", "opsess_acting", time.Now()); err != nil {
		t.Fatalf("RevokeAllForOperatorExcept: %v", err)
	}

	gotOtherOperator, err := repo.FindByTokenHash(context.Background(), otherOperator.TokenHash)
	if err != nil {
		t.Fatalf("find other operator's session: %v", err)
	}
	if gotOtherOperator == nil || gotOtherOperator.RevokedAt != nil {
		t.Errorf("expected a DIFFERENT operator's session to be untouched, RevokedAt = %+v", gotOtherOperator)
	}
}

func TestOperatorSessionRepository_RevokeAllForOperatorExcept_IsIdempotentOnASecondCall(t *testing.T) {
	repo := newTestOperatorSessionRepository(t)
	acting := testSessionForOperator("opsess_acting", "op_shared", "acting-token")
	other := testSessionForOperator("opsess_other", "op_shared", "other-token")
	if err := repo.Save(context.Background(), acting); err != nil {
		t.Fatalf("save acting: %v", err)
	}
	if err := repo.Save(context.Background(), other); err != nil {
		t.Fatalf("save other: %v", err)
	}
	firstRevoke := time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)
	secondRevoke := time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)
	if err := repo.RevokeAllForOperatorExcept(context.Background(), "op_shared", "opsess_acting", firstRevoke); err != nil {
		t.Fatalf("first RevokeAllForOperatorExcept: %v", err)
	}

	err := repo.RevokeAllForOperatorExcept(context.Background(), "op_shared", "opsess_acting", secondRevoke)

	if err != nil {
		t.Fatalf("second RevokeAllForOperatorExcept call: expected no error, got: %v", err)
	}
	gotOther, err := repo.FindByTokenHash(context.Background(), other.TokenHash)
	if err != nil {
		t.Fatalf("find other session: %v", err)
	}
	if gotOther.RevokedAt == nil || !gotOther.RevokedAt.Equal(firstRevoke) {
		t.Errorf("RevokedAt = %v, want it to stay at the first call's instant %v", gotOther.RevokedAt, firstRevoke)
	}
	gotActing, err := repo.FindByTokenHash(context.Background(), acting.TokenHash)
	if err != nil {
		t.Fatalf("find acting session: %v", err)
	}
	if gotActing.RevokedAt != nil {
		t.Errorf("expected the acting session to remain unrevoked across both calls, RevokedAt = %v", gotActing.RevokedAt)
	}
}
