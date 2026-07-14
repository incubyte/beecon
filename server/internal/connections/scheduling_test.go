// scheduling_test.go provides the shared fixture Slice 5's background-worker
// tests build on — scheduler_test.go (RefreshDueOnce) and
// reconcile_test.go (ReconcileOnce) — plus the exactly-once
// connection.expired concurrency tests that exercise both a scheduler claim
// and the request path (or reconciliation) racing on the very same
// connection (FD1). A schedulingOAuthClient scripts the refresh_token grant
// and the reconciliation user-info probe together (distinct from
// oauth_test.go's fakeOAuthClient and execution_access_test.go's
// refreshScriptedOAuthClient, neither of which need to be concurrency-safe or
// script the probe), and a fakeExpiredEventSink records every emitted event —
// both safe for concurrent use, since proving "exactly one" is this file's
// whole point.
package connections_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	memory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// schedulingOAuthClient scripts a fixed happy-path authorization_code
// exchange (every test here activates through it once), plus the
// refresh_token grant and the reconciliation user-info probe independently.
type schedulingOAuthClient struct {
	mu sync.Mutex

	refreshResult    connections.TokenExchangeResult
	refreshErr       error
	refreshCallCount int
	// refreshBlock, when non-nil, parks a RefreshGrant call after it has
	// already been counted and before it returns — the deterministic way
	// this file proves "only one grant between two concurrent callers": a
	// second caller's refreshOnce mutex Lock() is provably still queued
	// behind the first for as long as the first stays parked here (refreshOnce
	// holds that lock across the whole of refreshConnection, refreshlock.go).
	refreshBlock chan struct{}

	probeErr       error
	probeCallCount int
}

func (c *schedulingOAuthClient) ExchangeCode(_ context.Context, _ connections.TokenExchangeRequest) (connections.TokenExchangeResult, error) {
	return connections.TokenExchangeResult{AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600}, nil
}

func (c *schedulingOAuthClient) FetchAccount(_ context.Context, _ connections.AccountFetchRequest) (connections.AccountInfo, error) {
	return connections.AccountInfo{Email: "ada@example.com", DisplayName: "Ada"}, nil
}

func (c *schedulingOAuthClient) RefreshGrant(_ context.Context, _ connections.RefreshGrantRequest) (connections.TokenExchangeResult, error) {
	c.mu.Lock()
	c.refreshCallCount++
	block := c.refreshBlock
	result, err := c.refreshResult, c.refreshErr
	c.mu.Unlock()
	if block != nil {
		<-block
	}
	return result, err
}

func (c *schedulingOAuthClient) FetchUserInfo(_ context.Context, _, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probeCallCount++
	return c.probeErr
}

func (c *schedulingOAuthClient) setRefreshScript(result connections.TokenExchangeResult, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshResult, c.refreshErr = result, err
}

func (c *schedulingOAuthClient) RefreshCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refreshCallCount
}

func (c *schedulingOAuthClient) ProbeCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.probeCallCount
}

// fakeExpiredEventSink is a connections.EventSink test double recording every
// connection.expired event it observed, safe for concurrent use.
type fakeExpiredEventSink struct {
	mu     sync.Mutex
	events []connections.ExpiredEventData
}

func (s *fakeExpiredEventSink) ConnectionExpired(_ context.Context, _ organizations.OrgID, data connections.ExpiredEventData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, data)
	return nil
}

func (s *fakeExpiredEventSink) Events() []connections.ExpiredEventData {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]connections.ExpiredEventData, len(s.events))
	copy(out, s.events)
	return out
}

const (
	schedulingRefreshLead       = 10 * time.Minute
	schedulingReconcileInterval = 6 * time.Hour
)

var schedulingFixtureVaultKey = []byte("scheduling-fixture-vault-key-32!")

// schedulingFixture wires a connections.Facade with the in-memory Repository
// doubling as its RefreshQueue (mirrors driven/bun's own Repository/
// RefreshQueue split), a schedulingOAuthClient, and a fakeExpiredEventSink —
// the shared scaffolding every test in this file, scheduler_test.go, and
// reconcile_test.go builds on.
type schedulingFixture struct {
	facade *connections.Facade
	repo   *memory.Repository
	client *schedulingOAuthClient
	sink   *fakeExpiredEventSink
	clock  *mutableClock
	vault  *vault.Vault
}

func newSchedulingFixture(t *testing.T) *schedulingFixture {
	t.Helper()
	repo := memory.NewRepository()
	client := &schedulingOAuthClient{}
	sink := &fakeExpiredEventSink{}
	clock := &mutableClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	tokenVault, err := vault.NewVault(schedulingFixtureVaultKey)
	if err != nil {
		t.Fatalf("NewVault: %v", err)
	}

	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Repository: repo,
		Organizations: fakeOrgReader{orgs: map[organizations.OrgID]organizations.Organization{
			testOrg: {ID: testOrg, Name: "Acme", AllowedRedirectURIs: []string{allowedRedirect}},
		}},
		Users: fakeUserReader{users: map[organizations.UserID]organizations.User{
			testUser: {ID: testUser, OrgID: testOrg, Name: "Ada"},
		}},
		Integrations: fakeIntegrationReader{integrations: map[catalog.IntegrationID]catalog.Integration{
			testIntegration: {ID: testIntegration, ProviderSlug: testProviderSlug, ClientID: "the-client-id", ClientSecret: "the-client-secret"},
		}},
		Providers:   fakeProviderReader{definitions: map[string]catalog.ProviderDefinition{testProviderSlug: testProviderDefinition()}},
		OAuthClient: client,
		Vault:       tokenVault,
		Now:         clock.Now,
	}).WithScheduling(repo, sink, schedulingRefreshLead, schedulingReconcileInterval)

	return &schedulingFixture{facade: facade, repo: repo, client: client, sink: sink, clock: clock, vault: tokenVault}
}

// activate drives Initiate -> OpenConnectPage -> HandleCallback to an ACTIVE
// connection through f.client's fixed happy-path ExchangeCode/FetchAccount
// script (ExpiresIn 3600 — one hour from f.clock.now at the moment of
// activation).
func (f *schedulingFixture) activate(t *testing.T) connections.Connection {
	t.Helper()
	initiated, err := f.facade.Initiate(context.Background(), testOrg, testUser, testIntegration, allowedRedirect)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}
	view, err := f.facade.OpenConnectPage(context.Background(), initiated.Connection.ConnectToken)
	if err != nil {
		t.Fatalf("OpenConnectPage: %v", err)
	}
	state := queryParam(t, view.AuthorizeURL, "state")
	if _, err := f.facade.HandleCallback(context.Background(), "the-auth-code", state, ""); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	return f.get(t, initiated.Connection.ID)
}

func (f *schedulingFixture) get(t *testing.T, id connections.ConnectionID) connections.Connection {
	t.Helper()
	got, err := f.facade.Get(context.Background(), testOrg, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got
}

func (f *schedulingFixture) decrypt(t *testing.T, ciphertext string) string {
	t.Helper()
	plaintext, err := f.vault.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	return plaintext
}

// forceExpireToken persists connection with TokenExpiresAt moved into the
// past relative to f.clock.now — a token already actually expired (not
// merely "nearing" expiry), so a claim's refreshOnce call performs the grant
// regardless of needsRefresh's own gate. Tests that only care about a
// scheduler/reconciliation outcome (permanent refusal, transient failure,
// rotated token, one-grant-under-concurrency) use this to reach refreshOnce
// deterministically; TestRefreshDueOnce_RefreshesBeforeActualExpiry... (see
// scheduler_test.go) instead travels the clock to the "near, not yet
// expired" window AC1 actually promises.
func (f *schedulingFixture) forceExpireToken(t *testing.T, connection connections.Connection) {
	t.Helper()
	expiredAt := f.clock.now.Add(-time.Minute)
	connection.TokenExpiresAt = &expiredAt
	if err := f.repo.Update(context.Background(), connection); err != nil {
		t.Fatalf("seed already-expired token: %v", err)
	}
}

// waitForRefreshCallCount polls f.client.RefreshCallCount() until it reaches
// want, or fails the test after a generous timeout — used to know a
// concurrently launched RefreshGrant call has already been counted (and, when
// f.client.refreshBlock is set, is now parked holding the connection's
// refresh lock) before this file's exactly-once tests launch their second,
// concurrent caller.
func waitForRefreshCallCount(t *testing.T, client *schedulingOAuthClient, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.RefreshCallCount() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("RefreshCallCount never reached %d within the timeout (got %d)", want, client.RefreshCallCount())
}

// --- Exactly-once connection.expired under concurrency (FD1) ---

// TestConcurrentSchedulerAndRequestPath_PerformOnlyOneRefreshAndTheExecutionSucceeds
// is the success-path concurrency AC: a scheduled refresh (RefreshDueOnce)
// and a request-path resolve (ResolveForExecution) racing on the same
// connection must perform exactly one refresh_token grant between them, and
// the execution must succeed with the freshly refreshed token.
func TestConcurrentSchedulerAndRequestPath_PerformOnlyOneRefreshAndTheExecutionSucceeds(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.forceExpireToken(t, connection)
	f.client.refreshBlock = make(chan struct{})
	f.client.setRefreshScript(connections.TokenExchangeResult{AccessToken: "scheduled-refresh-token", RefreshToken: "raw-refresh-token", ExpiresIn: 3600}, nil)

	var wg sync.WaitGroup
	var refreshErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		refreshErr = f.facade.RefreshDueOnce(context.Background())
	}()
	waitForRefreshCallCount(t, f.client, 1) // the scheduler is now parked mid-grant, holding this connection's lock

	var access connections.ExecutionAccess
	var resolveErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		access, resolveErr = f.facade.ResolveForExecution(context.Background(), testOrg, testUser, connection.ID)
	}()
	time.Sleep(20 * time.Millisecond) // give the request path a chance to actually queue on the same lock
	close(f.client.refreshBlock)
	wg.Wait()

	if refreshErr != nil {
		t.Errorf("RefreshDueOnce: unexpected error: %v", refreshErr)
	}
	if resolveErr != nil {
		t.Errorf("ResolveForExecution: unexpected error: %v", resolveErr)
	}
	if access.Status != connections.StatusActive {
		t.Errorf("ResolveForExecution Status = %q, want %q — the concurrent execution must still succeed", access.Status, connections.StatusActive)
	}
	if access.AccessToken != "scheduled-refresh-token" {
		t.Errorf("ResolveForExecution AccessToken = %q, want the one grant's own token %q", access.AccessToken, "scheduled-refresh-token")
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Fatalf("RefreshGrant call count = %d, want exactly 1 between the scheduler and the request path", got)
	}
}

// TestConcurrentSchedulerAndRequestPath_BothDetectingAPermanentRefusalEmitExactlyOneEvent
// is FD1's core: a scheduler claim and a request-path resolve that both land
// on the very same ACTIVE->EXPIRED transition must emit connection.expired
// exactly once, no matter which path actually detected it.
func TestConcurrentSchedulerAndRequestPath_BothDetectingAPermanentRefusalEmitExactlyOneEvent(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.forceExpireToken(t, connection)
	f.client.refreshBlock = make(chan struct{})
	f.client.setRefreshScript(connections.TokenExchangeResult{}, connections.RefreshDenied{OAuthErrorCode: "invalid_grant"})

	var wg sync.WaitGroup
	var refreshErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		refreshErr = f.facade.RefreshDueOnce(context.Background())
	}()
	waitForRefreshCallCount(t, f.client, 1)

	var access connections.ExecutionAccess
	var resolveErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		access, resolveErr = f.facade.ResolveForExecution(context.Background(), testOrg, testUser, connection.ID)
	}()
	time.Sleep(20 * time.Millisecond)
	close(f.client.refreshBlock)
	wg.Wait()

	if refreshErr != nil {
		t.Errorf("RefreshDueOnce: unexpected error: %v", refreshErr)
	}
	if resolveErr != nil {
		t.Errorf("ResolveForExecution: unexpected error: %v — a permanent refusal must surface as a status, not an error", resolveErr)
	}
	if access.Status != connections.StatusExpired {
		t.Errorf("ResolveForExecution Status = %q, want %q", access.Status, connections.StatusExpired)
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Errorf("RefreshGrant call count = %d, want exactly 1", got)
	}
	if got := f.get(t, connection.ID).Status; got != connections.StatusExpired {
		t.Errorf("persisted Status = %q, want %q", got, connections.StatusExpired)
	}
	if events := f.sink.Events(); len(events) != 1 {
		t.Fatalf("connection.expired events = %d, want exactly 1 no matter which path detected the transition; events=%+v", len(events), events)
	}
}

// TestConcurrentReconciliationAndRequestPath_BothDetectingAPermanentRefusalEmitExactlyOneEvent
// is FD1's other convergence: reconciliation (PD37) and a forced request-path
// refresh (PD18's reactive 401 half, RefreshForExecution) racing on the same
// connection must still emit connection.expired exactly once.
func TestConcurrentReconciliationAndRequestPath_BothDetectingAPermanentRefusalEmitExactlyOneEvent(t *testing.T) {
	f := newSchedulingFixture(t)
	connection := f.activate(t)
	f.client.probeErr = connections.ErrProbeUnauthorized
	f.client.refreshBlock = make(chan struct{})
	f.client.setRefreshScript(connections.TokenExchangeResult{}, connections.RefreshDenied{OAuthErrorCode: "invalid_grant"})

	var wg sync.WaitGroup
	var reconcileErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		reconcileErr = f.facade.ReconcileOnce(context.Background())
	}()
	waitForRefreshCallCount(t, f.client, 1)

	var access connections.ExecutionAccess
	var refreshErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		access, refreshErr = f.facade.RefreshForExecution(context.Background(), testOrg, testUser, connection.ID)
	}()
	time.Sleep(20 * time.Millisecond)
	close(f.client.refreshBlock)
	wg.Wait()

	if reconcileErr != nil {
		t.Errorf("ReconcileOnce: unexpected error: %v", reconcileErr)
	}
	if refreshErr != nil {
		t.Errorf("RefreshForExecution: unexpected error: %v", refreshErr)
	}
	if access.Status != connections.StatusExpired {
		t.Errorf("RefreshForExecution Status = %q, want %q", access.Status, connections.StatusExpired)
	}
	if got := f.client.RefreshCallCount(); got != 1 {
		t.Errorf("RefreshGrant call count = %d, want exactly 1 between reconciliation and the request path", got)
	}
	if events := f.sink.Events(); len(events) != 1 {
		t.Fatalf("connection.expired events = %d, want exactly 1; events=%+v", len(events), events)
	}
}
