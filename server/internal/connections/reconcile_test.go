// reconcile_test.go exercises ReconcileOnce (reconcile.go, PD37) against the
// schedulingFixture shared with scheduler_test.go and scheduling_test.go
// (same package, scheduling_test.go's file header).
package connections_test

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/connections"
)

// TestReconcileOnce_ASuccessfulProbeMarksTheConnectionReconciledAndEmitsNoEvent
// is the happy path: a provider that still honors the connection's token
// just advances ReconciledAt — no status change, no event.
func TestReconcileOnce_ASuccessfulProbeMarksTheConnectionReconciledAndEmitsNoEvent(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.client.probeErr = nil

	if err := f.facade.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	if got := f.client.ProbeCallCount(); got != 1 {
		t.Fatalf("probe call count = %d, want exactly 1", got)
	}
	got := f.get(t, connection.ID)
	if got.Status != connections.StatusActive {
		t.Errorf("Status = %q, want %q — a successful probe must not disturb the connection", got.Status, connections.StatusActive)
	}
	if got.ReconciledAt == nil {
		t.Fatal("ReconciledAt is nil, want it advanced by a successful probe")
	}
	if len(f.sink.Events()) != 0 {
		t.Errorf("connection.expired events = %d, want 0", len(f.sink.Events()))
	}
}

// TestReconcileOnce_ARecentlyReconciledConnectionIsNotClaimedAgainWithinTheInterval
// proves ReconcileOnce respects BEECON_RECONCILE_INTERVAL: a connection just
// reconciled must not be probed again on the very next call.
func TestReconcileOnce_ARecentlyReconciledConnectionIsNotClaimedAgainWithinTheInterval(t *testing.T) {
	f := newSchedulingFixture(t)
	f.activate(t)
	if err := f.facade.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("first ReconcileOnce: %v", err)
	}
	if got := f.client.ProbeCallCount(); got != 1 {
		t.Fatalf("probe call count after first ReconcileOnce = %d, want 1", got)
	}

	if err := f.facade.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("second ReconcileOnce: %v", err)
	}

	if got := f.client.ProbeCallCount(); got != 1 {
		t.Errorf("probe call count after second ReconcileOnce = %d, want still 1 — a just-reconciled connection must not be re-claimed within the interval", got)
	}
}

// TestReconcileOnce_AnUnauthorizedProbeWithADeniedRefreshExpiresTheConnectionAndEmitsAnEvent
// is AC7/PD37/FD9's evidence rule satisfied: a 401-equivalent probe result
// (connections.ErrProbeUnauthorized) followed by a refused forced refresh is
// the only combination PD37 treats as a provider-side revocation — it must
// mark EXPIRED and deliver connection.expired.
func TestReconcileOnce_AnUnauthorizedProbeWithADeniedRefreshExpiresTheConnectionAndEmitsAnEvent(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.client.probeErr = connections.ErrProbeUnauthorized
	f.client.setRefreshScript(connections.TokenExchangeResult{}, connections.RefreshDenied{OAuthErrorCode: "invalid_grant"})

	if err := f.facade.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	got := f.get(t, connection.ID)
	if got.Status != connections.StatusExpired {
		t.Fatalf("Status = %q, want %q", got.Status, connections.StatusExpired)
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Errorf("RefreshGrant call count = %d, want exactly 1 (the forced confirmation refresh)", got)
	}
	events := f.sink.Events()
	if len(events) != 1 {
		t.Fatalf("connection.expired events = %d, want exactly 1", len(events))
	}
	if events[0].ConnectionID != string(connection.ID) {
		t.Errorf("data.connectionId = %q, want %q", events[0].ConnectionID, connection.ID)
	}
}

// TestReconcileOnce_ANetworkOrServerErrorProbeIsNotEvidenceAndLeavesTheConnectionActive
// is FD9's conservative-by-design rule: a probe failure that is NOT
// specifically connections.ErrProbeUnauthorized (a network error, a 5xx) must
// never be treated as revocation evidence — no forced refresh is even
// attempted, the connection stays ACTIVE, unreconciled (so the next scan
// tries again), and no event fires.
func TestReconcileOnce_ANetworkOrServerErrorProbeIsNotEvidenceAndLeavesTheConnectionActive(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.client.probeErr = errors.New("dial tcp: connection refused")

	if err := f.facade.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	got := f.get(t, connection.ID)
	if got.Status != connections.StatusActive {
		t.Fatalf("Status = %q, want %q — a network/5xx probe failure must never be treated as revocation evidence", got.Status, connections.StatusActive)
	}
	if got.ReconciledAt != nil {
		t.Error("ReconciledAt was set despite a probe failure that is not evidence — it must stay unreconciled so a later scan retries")
	}
	if got := f.client.RefreshCallCount(); got != 0 {
		t.Errorf("RefreshGrant call count = %d, want 0 — a non-evidence probe failure must never even attempt a confirmation refresh", got)
	}
	if len(f.sink.Events()) != 0 {
		t.Errorf("connection.expired events = %d, want 0", len(f.sink.Events()))
	}
}
