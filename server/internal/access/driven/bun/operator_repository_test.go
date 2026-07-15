// Package bun_test exercises the bun-backed OperatorRepository directly
// against a real SQLite database (mirroring connections/driven/bun's own
// repository_test.go convention): migration 0021's UNIQUE INDEX on email is
// a SQL-level guarantee the in-memory fake's plain map cannot prove.
package bun_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/access"
	accessbun "beecon/internal/access/driven/bun"
	"beecon/internal/db"
)

var operatorRepoTestDSNCounter int64

// newTestOperatorRepository boots a fresh in-memory SQLite database, runs
// the real embedded migrations (including 0021_operator_auth), and returns a
// bun-backed OperatorRepository.
func newTestOperatorRepository(t *testing.T) *accessbun.OperatorRepository {
	t.Helper()
	n := atomic.AddInt64(&operatorRepoTestDSNCounter, 1)
	dsn := fmt.Sprintf("file:operator_repo_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return accessbun.NewOperatorRepository(database)
}

func testOperator(id access.OperatorID, email string) access.Operator {
	return access.Operator{
		ID:           id,
		Email:        email,
		PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaA",
		Status:       access.OperatorStatusActive,
		CreatedAt:    time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC),
	}
}

func TestOperatorRepository_SaveThenFindByID_RoundTripsEveryField(t *testing.T) {
	repo := newTestOperatorRepository(t)
	operator := testOperator("op_1", "operator@example.com")

	if err := repo.Save(context.Background(), operator); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got == nil {
		t.Fatal("expected to find the saved operator, got nil")
	}
	if got.ID != operator.ID || got.Email != operator.Email || got.PasswordHash != operator.PasswordHash || got.Status != operator.Status {
		t.Errorf("FindByID = %+v, want %+v", *got, operator)
	}
	if !got.CreatedAt.Equal(operator.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, operator.CreatedAt)
	}
}

func TestOperatorRepository_FindByID_ReturnsNilNilForAnUnknownID(t *testing.T) {
	repo := newTestOperatorRepository(t)

	got, err := repo.FindByID(context.Background(), "op_does-not-exist")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for an unknown id, got %+v", got)
	}
}

func TestOperatorRepository_FindByEmail_FindsTheOperatorByItsStoredNormalizedEmail(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.FindByEmail(context.Background(), "operator@example.com")

	if err != nil {
		t.Fatalf("find by email: %v", err)
	}
	if got == nil || got.ID != "op_1" {
		t.Fatalf("FindByEmail = %+v, want the op_1 operator", got)
	}
}

func TestOperatorRepository_FindByEmail_ReturnsNilNilForAnUnknownEmail(t *testing.T) {
	repo := newTestOperatorRepository(t)

	got, err := repo.FindByEmail(context.Background(), "nobody@example.com")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for an unknown email, got %+v", got)
	}
}

func TestOperatorRepository_Exists_FalseWhenNoOperatorHasEverBeenSaved(t *testing.T) {
	repo := newTestOperatorRepository(t)

	exists, err := repo.Exists(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected Exists to be false against an empty operator_accounts table")
	}
}

func TestOperatorRepository_Exists_TrueOnceAnOperatorHasBeenSaved(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}

	exists, err := repo.Exists(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected Exists to be true once an operator row exists")
	}
}

// TestOperatorRepository_Save_RejectsADuplicateEmailViaTheUniqueIndex pins
// migration 0021's `UNIQUE INDEX idx_operator_accounts_email` at the real-SQL
// level: the facade normalizes (lowercases) email before ever calling Save,
// so it is this unique index — not application code — that is the last line
// of defense making "duplicate email, case included" impossible to persist.
func TestOperatorRepository_Save_RejectsADuplicateEmailViaTheUniqueIndex(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("first save: %v", err)
	}

	err := repo.Save(context.Background(), testOperator("op_2", "operator@example.com"))

	if err == nil {
		t.Fatal("expected the second Save with a duplicate email to fail the unique index constraint, got nil error")
	}
}

// --- ListAll/UpdatePasswordHash/SetStatus/CountActive (Slice 4 additions,
// real SQLite). ---

func TestOperatorRepository_ListAll_ReturnsEverySavedOperator(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "first@example.com")); err != nil {
		t.Fatalf("save op_1: %v", err)
	}
	if err := repo.Save(context.Background(), testOperator("op_2", "second@example.com")); err != nil {
		t.Fatalf("save op_2: %v", err)
	}

	operators, err := repo.ListAll(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(operators) != 2 {
		t.Fatalf("got %d operators, want 2", len(operators))
	}
}

func TestOperatorRepository_ListAll_ReturnsAnEmptySliceWhenNoOperatorExists(t *testing.T) {
	repo := newTestOperatorRepository(t)

	operators, err := repo.ListAll(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(operators) != 0 {
		t.Errorf("got %d operators, want 0", len(operators))
	}
}

func TestOperatorRepository_UpdatePasswordHash_OverwritesTheStoredHash(t *testing.T) {
	repo := newTestOperatorRepository(t)
	original := testOperator("op_1", "operator@example.com")
	if err := repo.Save(context.Background(), original); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := repo.UpdatePasswordHash(context.Background(), "op_1", "$argon2id$v=19$m=19456,t=2,p=1$bmV3c2FsdG5ld3NhbHQ$bmV3aGFzaG5ld2hhc2huZXdoYXNo"); err != nil {
		t.Fatalf("UpdatePasswordHash: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.PasswordHash == original.PasswordHash {
		t.Error("expected PasswordHash to change, got the original hash still stored")
	}
	if got.PasswordHash != "$argon2id$v=19$m=19456,t=2,p=1$bmV3c2FsdG5ld3NhbHQ$bmV3aGFzaG5ld2hhc2huZXdoYXNo" {
		t.Errorf("PasswordHash = %q, want the newly set hash", got.PasswordHash)
	}
}

func TestOperatorRepository_SetStatus_OverwritesTheStoredStatus(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := repo.SetStatus(context.Background(), "op_1", access.OperatorStatusDisabled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.Status != access.OperatorStatusDisabled {
		t.Errorf("Status = %q, want %q", got.Status, access.OperatorStatusDisabled)
	}
}

func TestOperatorRepository_CountActive_CountsOnlyStillActiveOperators(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "first@example.com")); err != nil {
		t.Fatalf("save op_1: %v", err)
	}
	if err := repo.Save(context.Background(), testOperator("op_2", "second@example.com")); err != nil {
		t.Fatalf("save op_2: %v", err)
	}
	if err := repo.SetStatus(context.Background(), "op_2", access.OperatorStatusDisabled); err != nil {
		t.Fatalf("disable op_2: %v", err)
	}

	count, err := repo.CountActive(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("CountActive = %d, want 1 (op_2 was disabled)", count)
	}
}

func TestOperatorRepository_CountActive_IsZeroWhenNoOperatorExists(t *testing.T) {
	repo := newTestOperatorRepository(t)

	count, err := repo.CountActive(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("CountActive = %d, want 0", count)
	}
}

// --- RecordFailedAttempt/ResetFailedAttempts (Slice 5 additions, real
// SQLite, migration 0022's failed_attempts/locked_until columns). ---

func TestOperatorRepository_RecordFailedAttempt_IncrementsFailedAttemptsByOne(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := repo.RecordFailedAttempt(context.Background(), "op_1", nil); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.FailedAttempts != 1 {
		t.Errorf("FailedAttempts = %d, want 1", got.FailedAttempts)
	}
	if got.LockedUntil != nil {
		t.Errorf("LockedUntil = %v, want nil (a nil lockedUntil argument must not set the column)", got.LockedUntil)
	}
}

func TestOperatorRepository_RecordFailedAttempt_AccumulatesAcrossMultipleCalls(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := repo.RecordFailedAttempt(context.Background(), "op_1", nil); err != nil {
			t.Fatalf("RecordFailedAttempt call %d: %v", i+1, err)
		}
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.FailedAttempts != 3 {
		t.Errorf("FailedAttempts = %d, want 3 (three separate increments)", got.FailedAttempts)
	}
}

func TestOperatorRepository_RecordFailedAttempt_SetsLockedUntilWhenGivenANonNilValue(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}
	lockedUntil := time.Date(2026, 7, 15, 9, 15, 0, 0, time.UTC)

	if err := repo.RecordFailedAttempt(context.Background(), "op_1", &lockedUntil); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.LockedUntil == nil || !got.LockedUntil.Equal(lockedUntil) {
		t.Errorf("LockedUntil = %v, want %v", got.LockedUntil, lockedUntil)
	}
}

func TestOperatorRepository_ResetFailedAttempts_ZeroesTheCounterAndClearsLockedUntil(t *testing.T) {
	repo := newTestOperatorRepository(t)
	if err := repo.Save(context.Background(), testOperator("op_1", "operator@example.com")); err != nil {
		t.Fatalf("save: %v", err)
	}
	lockedUntil := time.Date(2026, 7, 15, 9, 15, 0, 0, time.UTC)
	if err := repo.RecordFailedAttempt(context.Background(), "op_1", &lockedUntil); err != nil {
		t.Fatalf("RecordFailedAttempt fixture: %v", err)
	}

	if err := repo.ResetFailedAttempts(context.Background(), "op_1"); err != nil {
		t.Fatalf("ResetFailedAttempts: %v", err)
	}

	got, err := repo.FindByID(context.Background(), "op_1")
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if got.FailedAttempts != 0 {
		t.Errorf("FailedAttempts = %d, want 0", got.FailedAttempts)
	}
	if got.LockedUntil != nil {
		t.Errorf("LockedUntil = %v, want nil (cleared)", got.LockedUntil)
	}
}

func TestOperatorRepository_RecordFailedAttemptAndResetFailedAttempts_AreNoOpsForAnUnknownID(t *testing.T) {
	repo := newTestOperatorRepository(t)

	if err := repo.RecordFailedAttempt(context.Background(), "op_does-not-exist", nil); err != nil {
		t.Errorf("RecordFailedAttempt for an unknown id: unexpected error: %v", err)
	}
	if err := repo.ResetFailedAttempts(context.Background(), "op_does-not-exist"); err != nil {
		t.Errorf("ResetFailedAttempts for an unknown id: unexpected error: %v", err)
	}
}
