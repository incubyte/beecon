// scheduler_test.go exercises RefreshDueOnce (scheduler.go, PD36) against the
// schedulingFixture shared with reconcile_test.go and scheduling_test.go
// (same package, scheduling_test.go's file header).
package connections_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/connections"
)

// TestRefreshDueOnce_DoesNotClaimAConnectionFarFromExpiry proves the negative
// space: a connection whose token has plenty of life left (outside the
// refresh lead) must never be touched by a scan.
func TestRefreshDueOnce_DoesNotClaimAConnectionFarFromExpiry(t *testing.T) {
	f := newSchedulingFixture(t)
	f.activate(t) // TokenExpiresAt is one hour out; f.clock.now never advances

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}
	if got := f.client.RefreshCallCount(); got != 0 {
		t.Errorf("RefreshGrant call count = %d, want 0 — a connection far from expiry must not be claimed", got)
	}
}

// TestRefreshDueOnce_RefreshesBeforeActualExpirySoARequestRightAfterExpiryTimeNeverTriggersARequestPathRefresh
// is AC1, verbatim — the slice's own success criterion: "expiry is invisible
// while refresh works". A background scan sees the token is nearing expiry
// (inside BEECON_REFRESH_LEAD) and refreshes it *before* the token's own
// expiry instant; a consumer resolving the connection right after that
// original expiry-time instant must get a normal ACTIVE success without the
// request path ever calling RefreshGrant a second time.
func TestRefreshDueOnce_RefreshesBeforeActualExpirySoARequestRightAfterExpiryTimeNeverTriggersARequestPathRefresh(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t) // TokenExpiresAt = now + 1h
	originalExpiry := *f.get(t, connection.ID).TokenExpiresAt
	f.clock.now = f.clock.now.Add(55 * time.Minute) // 5 minutes still remain — inside the 10-minute refresh lead, NOT yet expired
	f.client.setRefreshScript(connections.TokenExchangeResult{AccessToken: "scheduled-refresh-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600}, nil)

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Fatalf("RefreshGrant call count = %d, want exactly 1 — the scheduler must refresh a connection nearing expiry, before it actually expires", got)
	}

	// Travel to just past the ORIGINAL expiry instant — the moment a
	// request-path refresh would otherwise have been forced.
	f.clock.now = originalExpiry.Add(time.Minute)
	access, err := f.facade.ResolveForExecution(context.Background(), testOrg, testUser, connection.ID)

	if err != nil {
		t.Fatalf("ResolveForExecution: %v", err)
	}
	if access.Status != connections.StatusActive {
		t.Fatalf("Status = %q, want %q", access.Status, connections.StatusActive)
	}
	if access.AccessToken != "scheduled-refresh-token" {
		t.Errorf("AccessToken = %q, want the scheduled refresh's own token", access.AccessToken)
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Errorf("RefreshGrant call count after resolving = %d, want still exactly 1 — the request path must never refresh again", got)
	}
}

// TestRefreshDueOnce_ScheduledRotatedRefreshTokenReplacesTheStoredOne pins the
// "during a scheduled refresh" wording of Slice 5's own AC (rotated-token
// replacement is already pinned generically for refreshConnection via
// RefreshForExecution in execution_access_test.go; this test proves the same
// funnel through the scheduler's own entry point).
func TestRefreshDueOnce_ScheduledRotatedRefreshTokenReplacesTheStoredOne(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.forceExpireToken(t, connection)
	f.client.setRefreshScript(connections.TokenExchangeResult{AccessToken: "scheduled-access-token", RefreshToken: "rotated-refresh-token", ExpiresIn: 3600}, nil)

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}

	got := f.get(t, connection.ID)
	if plaintext := f.decrypt(t, got.EncryptedRefreshToken); plaintext != "rotated-refresh-token" {
		t.Errorf("decrypted refresh token = %q, want the provider's rotated value", plaintext)
	}
}

// TestRefreshDueOnce_APermanentRefusalExpiresTheConnectionAndEmitsThePD32DataShape
// is AC3: invalid_grant-and-kin marks EXPIRED and delivers connection.expired
// carrying exactly PD32's fields.
func TestRefreshDueOnce_APermanentRefusalExpiresTheConnectionAndEmitsThePD32DataShape(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.forceExpireToken(t, connection)
	f.client.setRefreshScript(connections.TokenExchangeResult{}, connections.RefreshDenied{OAuthErrorCode: "invalid_grant"})

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}

	got := f.get(t, connection.ID)
	if got.Status != connections.StatusExpired {
		t.Fatalf("Status = %q, want %q", got.Status, connections.StatusExpired)
	}
	events := f.sink.Events()
	if len(events) != 1 {
		t.Fatalf("connection.expired events = %d, want exactly 1", len(events))
	}
	event := events[0]
	if event.ConnectionID != string(connection.ID) {
		t.Errorf("data.connectionId = %q, want %q", event.ConnectionID, connection.ID)
	}
	if event.UserID != string(testUser) {
		t.Errorf("data.userId = %q, want %q", event.UserID, testUser)
	}
	if event.IntegrationID != string(testIntegration) {
		t.Errorf("data.integrationId = %q, want %q", event.IntegrationID, testIntegration)
	}
	if event.ProviderSlug != testProviderSlug {
		t.Errorf("data.providerSlug = %q, want %q", event.ProviderSlug, testProviderSlug)
	}
	if event.Reason == "" {
		t.Error("data.reason is empty, want a non-empty reason")
	}
}

// TestRefreshDueOnce_ATransientFailureLeavesTheConnectionActiveAndRetriesOnALaterScan
// is AC4: a network/5xx-style failure must not expire the connection, and a
// later scan (after this scan's own claim lease has released) must retry —
// exactly the behavior FD3 changes on top of Phase 2's expire-on-any-error.
func TestRefreshDueOnce_ATransientFailureLeavesTheConnectionActiveAndRetriesOnALaterScan(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.forceExpireToken(t, connection)
	f.client.setRefreshScript(connections.TokenExchangeResult{}, errors.New("connection reset by peer"))

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("RefreshDueOnce: %v", err)
	}
	if got := f.get(t, connection.ID).Status; got != connections.StatusActive {
		t.Fatalf("Status = %q, want %q — a transient failure must leave the connection untouched", got, connections.StatusActive)
	}
	if len(f.sink.Events()) != 0 {
		t.Fatalf("connection.expired events = %d, want 0 for a transient failure", len(f.sink.Events()))
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Fatalf("RefreshGrant call count = %d, want exactly 1 so far", got)
	}

	// A later scan, well past this scan's own claim lease, must retry.
	f.clock.now = f.clock.now.Add(time.Minute)
	f.client.setRefreshScript(connections.TokenExchangeResult{AccessToken: "recovered-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600}, nil)

	if err := f.facade.RefreshDueOnce(context.Background()); err != nil {
		t.Fatalf("second RefreshDueOnce: %v", err)
	}
	if got := f.client.RefreshCallCount(); got != 2 {
		t.Fatalf("RefreshGrant call count after the later scan = %d, want exactly 2 (the retry)", got)
	}
	got := f.get(t, connection.ID)
	if got.Status != connections.StatusActive {
		t.Fatalf("Status = %q, want %q after the retry succeeds", got.Status, connections.StatusActive)
	}
	if plaintext := f.decrypt(t, got.EncryptedAccessToken); plaintext != "recovered-access-token" {
		t.Errorf("decrypted access token = %q, want the retry's own recovered token", plaintext)
	}
}
