// Package bun_test exercises the bun-backed Repository directly against a
// real SQLite database (the project's own test dialect) rather than the
// driven/memory fake: MarkStateConsumed's compare-and-set update is a SQL-level
// guarantee (WHERE consumed_at IS NULL) that the in-memory fake's simple map
// mutation cannot prove — a state must never be consumed twice, even when two
// callbacks race on it (Slice 4/5, AC7).
package bun_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/connections"
	connectionsbun "beecon/internal/connections/driven/bun"
	"beecon/internal/db"
)

// testDSNCounter guarantees a fresh, unshared in-memory database per test in
// this file, mirroring test/support.NewTestDSN's own reasoning.
var testDSNCounter int64

// newTestRepository boots a fresh in-memory SQLite database, runs the real
// embedded migrations, and returns a bun-backed Repository. MaxOpenConns is
// pinned to 1 so two goroutines calling MarkStateConsumed "concurrently"
// serialize at the connection pool rather than risking a SQLITE_BUSY error —
// the assertion under test is the CAS update's atomicity, not true OS-thread
// parallelism.
func newTestRepository(t *testing.T) *connectionsbun.Repository {
	t.Helper()
	n := atomic.AddInt64(&testDSNCounter, 1)
	dsn := fmt.Sprintf("file:oauth_state_cas_test_%d?mode=memory&cache=shared", n)
	database, err := db.New("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	if err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return connectionsbun.NewRepository(database)
}

// seedConnectionAndState inserts a minimal Connection and its bound OAuthState
// (unconsumed), returning the state's token so a test can call
// MarkStateConsumed against it.
func seedConnectionAndState(t *testing.T, repo *connectionsbun.Repository, connID connections.ConnectionID, state string) {
	t.Helper()
	ctx := context.Background()
	connection := connections.Connection{
		ID:           connID,
		OrgID:        "org_1",
		UserID:       "user_1",
		ProviderSlug: "outlook",
		Status:       connections.StatusInitiated,
		RedirectURI:  "https://consumer.example.com/callback",
		ConnectToken: "connect-token-" + string(connID),
		CreatedAt:    time.Now().UTC(),
	}
	if err := repo.Save(ctx, connection); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	oauthState := connections.OAuthState{
		State:        state,
		ConnectionID: connID,
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}
	if err := repo.SaveState(ctx, oauthState); err != nil {
		t.Fatalf("seed oauth state: %v", err)
	}
}

func TestMarkStateConsumed_SucceedsOnce(t *testing.T) {
	repo := newTestRepository(t)
	seedConnectionAndState(t, repo, "conn_seq", "state_seq")

	err := repo.MarkStateConsumed(context.Background(), "state_seq", time.Now().UTC())

	if err != nil {
		t.Fatalf("first MarkStateConsumed: unexpected error: %v", err)
	}
	got, err := repo.FindState(context.Background(), "state_seq")
	if err != nil {
		t.Fatalf("FindState: %v", err)
	}
	if got == nil || got.ConsumedAt == nil {
		t.Fatal("expected the state to be marked consumed")
	}
}

// TestMarkStateConsumed_ASecondCallOnAnAlreadyConsumedStateReturnsErrStateAlreadyUsed
// pins the CAS's core guarantee at the SQL level: WHERE consumed_at IS NULL
// matches zero rows the second time, so the repository must translate that
// into ErrStateAlreadyUsed rather than silently succeeding again.
func TestMarkStateConsumed_ASecondCallOnAnAlreadyConsumedStateReturnsErrStateAlreadyUsed(t *testing.T) {
	repo := newTestRepository(t)
	seedConnectionAndState(t, repo, "conn_seq2", "state_seq2")
	if err := repo.MarkStateConsumed(context.Background(), "state_seq2", time.Now().UTC()); err != nil {
		t.Fatalf("first MarkStateConsumed: unexpected error: %v", err)
	}

	err := repo.MarkStateConsumed(context.Background(), "state_seq2", time.Now().UTC())

	if err == nil {
		t.Fatal("expected the second MarkStateConsumed call to fail")
	}
	de := connections.ErrStateAlreadyUsed()
	if err.Error() != de.Error() {
		t.Errorf("error = %v, want ErrStateAlreadyUsed (%v)", err, de)
	}
}

// TestMarkStateConsumed_ExactlyOneOfTwoConcurrentCallsSucceeds is AC7's race
// scenario: two callbacks racing to consume the same CSRF state must never
// both succeed — exactly one MarkStateConsumed call wins, the other gets
// ErrStateAlreadyUsed, so the connection can never be activated twice.
func TestMarkStateConsumed_ExactlyOneOfTwoConcurrentCallsSucceeds(t *testing.T) {
	repo := newTestRepository(t)
	seedConnectionAndState(t, repo, "conn_race", "state_race")

	const attempts = 5
	results := make([]error, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = repo.MarkStateConsumed(context.Background(), "state_race", time.Now().UTC())
		}(i)
	}
	close(start)
	wg.Wait()

	successCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		}
	}
	if successCount != 1 {
		t.Fatalf("got %d successful MarkStateConsumed calls out of %d concurrent attempts, want exactly 1", successCount, attempts)
	}
}
