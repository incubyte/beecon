// lease_claims_test.go exercises TransitionStatus's conditional-flip guarantee
// (FD1) and ClaimDueRefresh/ClaimDueReconcile's lease semantics (PD29/PD36/
// PD37) directly against a real SQLite database — the same "prove the SQL-
// level guarantee, not just the in-memory fake's map mutation" reasoning
// repository_test.go's MarkStateConsumed tests already apply, extended to
// Slice 5's own claim queries: a claimed connection must not be re-claimable
// until its lease expires, and a connection that is not ACTIVE must never be
// claimed at all, no matter how due its token/reconciliation columns look.
package bun_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"beecon/internal/connections"
	connectionsbun "beecon/internal/connections/driven/bun"
)

const (
	leaseTestLead      = 10 * time.Minute
	leaseTestLeaseTTL  = 30 * time.Second
	leaseTestInterval  = 6 * time.Hour
	leaseTestBatchSize = 50
)

// seedConnection inserts a minimal but fully-formed Connection row (Save
// persists Status/TokenExpiresAt/ReconciledAt exactly as given — no OAuth
// handshake needed to seed the shapes these claim queries key off).
func seedConnection(t *testing.T, repo *connectionsbun.Repository, id connections.ConnectionID, status connections.Status, tokenExpiresAt, reconciledAt *time.Time) {
	t.Helper()
	connection := connections.Connection{
		ID:                    id,
		OrgID:                 "org_1",
		UserID:                "user_1",
		ProviderSlug:          "outlook",
		Status:                status,
		RedirectURI:           "https://consumer.example.com/callback",
		ConnectToken:          "connect-token-" + string(id),
		EncryptedAccessToken:  "encrypted-access-token",
		EncryptedRefreshToken: "encrypted-refresh-token",
		TokenExpiresAt:        tokenExpiresAt,
		ReconciledAt:          reconciledAt,
		CreatedAt:             time.Now().UTC(),
	}
	if err := repo.Save(context.Background(), connection); err != nil {
		t.Fatalf("seed connection %q: %v", id, err)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

// --- ClaimDueRefresh (PD36) ---

func TestClaimDueRefresh_ClaimsOnlyAnActiveConnectionWhoseTokenNearsExpiryNotADisabledOne(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_active_due", connections.StatusActive, timePtr(now.Add(2*time.Minute)), nil)
	seedConnection(t, repo, "conn_disconnected_due", connections.StatusDisconnected, timePtr(now.Add(2*time.Minute)), nil)

	due, err := repo.ClaimDueRefresh(context.Background(), now, leaseTestLead, leaseTestLeaseTTL, leaseTestBatchSize)

	if err != nil {
		t.Fatalf("ClaimDueRefresh: %v", err)
	}
	if len(due) != 1 || due[0].ID != "conn_active_due" {
		t.Fatalf("claimed = %+v, want exactly the ACTIVE connection — a non-ACTIVE connection must never be claimed regardless of its token_expires_at", due)
	}
}

// TestClaimDueRefresh_ASecondClaimBeforeTheLeaseExpiresDoesNotReclaimTheSameConnection
// is the lease's own crash-safety guarantee: a row claimed by one caller must
// not be handed to a second caller until the lease TTL elapses.
func TestClaimDueRefresh_ASecondClaimBeforeTheLeaseExpiresDoesNotReclaimTheSameConnection(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_leased", connections.StatusActive, timePtr(now.Add(time.Minute)), nil)

	first, err := repo.ClaimDueRefresh(context.Background(), now, leaseTestLead, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("first ClaimDueRefresh: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly 1", first)
	}

	second, err := repo.ClaimDueRefresh(context.Background(), now, leaseTestLead, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("second ClaimDueRefresh: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim (before the lease expires) = %+v, want 0 — a leased row must not be re-claimable yet", second)
	}

	afterLease := now.Add(leaseTestLeaseTTL + time.Second)
	third, err := repo.ClaimDueRefresh(context.Background(), afterLease, leaseTestLead, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("third ClaimDueRefresh: %v", err)
	}
	if len(third) != 1 || third[0].ID != "conn_leased" {
		t.Fatalf("third claim (after the lease expires) = %+v, want the same connection to be re-claimable", third)
	}
}

// TestClaimDueRefresh_ExactlyOneOfTwoConcurrentClaimsWinsTheSameConnection
// mirrors MarkStateConsumed's own race test: two callers racing to claim the
// same due connection must never both win it.
func TestClaimDueRefresh_ExactlyOneOfTwoConcurrentClaimsWinsTheSameConnection(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_race", connections.StatusActive, timePtr(now.Add(time.Minute)), nil)

	const attempts = 5
	results := make([][]connections.Connection, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claimed, err := repo.ClaimDueRefresh(context.Background(), now, leaseTestLead, leaseTestLeaseTTL, leaseTestBatchSize)
			if err != nil {
				t.Errorf("ClaimDueRefresh: %v", err)
				return
			}
			results[i] = claimed
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for _, claimed := range results {
		winners += len(claimed)
	}
	if winners != 1 {
		t.Fatalf("total connections claimed across %d concurrent attempts = %d, want exactly 1", attempts, winners)
	}
}

// --- ClaimDueReconcile (PD37) ---

func TestClaimDueReconcile_ClaimsOnlyAnActiveConnectionDueForReconciliationNotADisabledOne(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_active_unreconciled", connections.StatusActive, nil, nil)
	seedConnection(t, repo, "conn_disconnected_unreconciled", connections.StatusDisconnected, nil, nil)

	due, err := repo.ClaimDueReconcile(context.Background(), now, leaseTestInterval, leaseTestLeaseTTL, leaseTestBatchSize)

	if err != nil {
		t.Fatalf("ClaimDueReconcile: %v", err)
	}
	if len(due) != 1 || due[0].ID != "conn_active_unreconciled" {
		t.Fatalf("claimed = %+v, want exactly the ACTIVE connection", due)
	}
}

func TestClaimDueReconcile_ASecondClaimBeforeTheLeaseExpiresDoesNotReclaimTheSameConnection(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_reconcile_leased", connections.StatusActive, nil, nil)

	first, err := repo.ClaimDueReconcile(context.Background(), now, leaseTestInterval, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("first ClaimDueReconcile: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly 1", first)
	}

	second, err := repo.ClaimDueReconcile(context.Background(), now, leaseTestInterval, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("second ClaimDueReconcile: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim (before the lease expires) = %+v, want 0", second)
	}

	afterLease := now.Add(leaseTestLeaseTTL + time.Second)
	third, err := repo.ClaimDueReconcile(context.Background(), afterLease, leaseTestInterval, leaseTestLeaseTTL, leaseTestBatchSize)
	if err != nil {
		t.Fatalf("third ClaimDueReconcile: %v", err)
	}
	if len(third) != 1 || third[0].ID != "conn_reconcile_leased" {
		t.Fatalf("third claim (after the lease expires) = %+v, want the same connection to be re-claimable", third)
	}
}

// TestClaimDueReconcile_ARecentlyReconciledConnectionIsNotDueWithinTheInterval
// pins the "not probed within BEECON_RECONCILE_INTERVAL" half directly (the
// in-memory fake's own equivalent lives in reconcile_test.go).
func TestClaimDueReconcile_ARecentlyReconciledConnectionIsNotDueWithinTheInterval(t *testing.T) {
	repo := newTestRepository(t)
	now := time.Now().UTC()
	seedConnection(t, repo, "conn_recently_reconciled", connections.StatusActive, nil, timePtr(now.Add(-time.Minute)))

	due, err := repo.ClaimDueReconcile(context.Background(), now, leaseTestInterval, leaseTestLeaseTTL, leaseTestBatchSize)

	if err != nil {
		t.Fatalf("ClaimDueReconcile: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("claimed = %+v, want 0 — a connection reconciled a minute ago must not be due again within a 6h interval", due)
	}
}

// --- TransitionStatus (FD1) ---

func TestTransitionStatus_FlipsOnlyWhenTheCurrentStatusMatchesFrom(t *testing.T) {
	repo := newTestRepository(t)
	seedConnection(t, repo, "conn_transition", connections.StatusActive, nil, nil)

	flipped, err := repo.TransitionStatus(context.Background(), "org_1", "conn_transition", connections.StatusActive, connections.StatusExpired)
	if err != nil {
		t.Fatalf("TransitionStatus: %v", err)
	}
	if !flipped {
		t.Fatal("expected the flip to report true when the current status matches from")
	}
	got, err := repo.FindByID(context.Background(), "org_1", "conn_transition")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Status != connections.StatusExpired {
		t.Errorf("persisted Status = %q, want %q", got.Status, connections.StatusExpired)
	}
}

// TestTransitionStatus_ASecondCallOnAnAlreadyTransitionedConnectionReportsFalseAndDoesNotDoubleFlip
// is FD1's exactly-once guarantee at the SQL level: once ACTIVE->EXPIRED has
// already happened, a second identical call must report false (no rows
// matched status='ACTIVE' anymore) rather than silently succeeding again.
func TestTransitionStatus_ASecondCallOnAnAlreadyTransitionedConnectionReportsFalseAndDoesNotDoubleFlip(t *testing.T) {
	repo := newTestRepository(t)
	seedConnection(t, repo, "conn_already_transitioned", connections.StatusActive, nil, nil)
	if flipped, err := repo.TransitionStatus(context.Background(), "org_1", "conn_already_transitioned", connections.StatusActive, connections.StatusExpired); err != nil || !flipped {
		t.Fatalf("first TransitionStatus: flipped=%v err=%v, want true, nil", flipped, err)
	}

	flipped, err := repo.TransitionStatus(context.Background(), "org_1", "conn_already_transitioned", connections.StatusActive, connections.StatusExpired)

	if err != nil {
		t.Fatalf("second TransitionStatus: %v", err)
	}
	if flipped {
		t.Fatal("expected the second TransitionStatus call to report false — the connection is no longer ACTIVE")
	}
}

// TestTransitionStatus_ExactlyOneOfTwoConcurrentCallsReportsTheFlip is FD1's
// concurrency guarantee proved at the SQL level, independent of the
// application-level per-connection lock (refreshlock.go) that normally
// prevents this race from even reaching two simultaneous SQL calls.
func TestTransitionStatus_ExactlyOneOfTwoConcurrentCallsReportsTheFlip(t *testing.T) {
	repo := newTestRepository(t)
	seedConnection(t, repo, "conn_transition_race", connections.StatusActive, nil, nil)

	const attempts = 5
	results := make([]bool, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			flipped, err := repo.TransitionStatus(context.Background(), "org_1", "conn_transition_race", connections.StatusActive, connections.StatusExpired)
			if err != nil {
				t.Errorf("TransitionStatus: %v", err)
				return
			}
			results[i] = flipped
		}(i)
	}
	close(start)
	wg.Wait()

	flips := 0
	for _, flipped := range results {
		if flipped {
			flips++
		}
	}
	if flips != 1 {
		t.Fatalf("flips reported true across %d concurrent attempts = %d, want exactly 1", attempts, flips)
	}
}
