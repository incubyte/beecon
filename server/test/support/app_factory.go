//go:build integration

// Package support provides the integration-test harness. The app is booted
// through the real composition root (app.Wire) against a SQLite in-memory
// database (mirroring the sqlite adapter tests run on), so journey tests
// exercise the production wiring end to end: config -> db connect -> boot
// migrations -> facades -> handlers -> router.
package support

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/internal/config"
)

// AdminAPIKey is the installation admin key every test-booted app is
// configured with.
const AdminAPIKey = "test-admin-key"

// EncryptionKeyBase64 is a valid 32-byte base64-encoded token-encryption key
// (BEECON_ENCRYPTION_KEY, PD12) every test-booted app boots with.
const EncryptionKeyBase64 = "c3VwcG9ydC10ZXN0LWVuY3J5cHRpb24ta2V5LTMyISE="

// testDSNCounter guarantees a fresh, unshared in-memory database per call to
// NewTestDSN even when tests run in parallel or share a t.Name() prefix.
var testDSNCounter int64

// NewTestDSN returns a fresh SQLite in-memory DSN unique to the calling test.
// "cache=shared" keeps the in-memory database alive across multiple bun.DB
// connections (and, for restart-style journeys, across a second BootAppAt
// call) as long as at least one connection to it remains open.
func NewTestDSN(t *testing.T) string {
	t.Helper()
	n := atomic.AddInt64(&testDSNCounter, 1)
	name := sanitizeForDSN(t.Name())
	return fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", name, n)
}

// BootApp boots the full app against a fresh, uniquely named SQLite
// in-memory database.
func BootApp(t *testing.T) *app.Wired {
	t.Helper()
	return BootAppAt(t, NewTestDSN(t))
}

// BootAppAt boots the full app against the given DSN, so a test can "restart"
// the app against the same database by calling BootAppAt twice with the same
// dsn. The DB connection is registered for cleanup with t; the caller must
// keep the first Wired's connection open (i.e. not call Close manually)
// until the second boot has happened, or the shared in-memory database is
// dropped.
func BootAppAt(t *testing.T, dsn string) *app.Wired {
	t.Helper()
	ctx := context.Background()

	wired, err := app.Wire(ctx, app.Deps{
		Config: testConfig(t, dsn),
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// BootAppWithProviderDefinitions boots the full app against a fresh SQLite
// in-memory database, overriding the loaded provider definitions — e.g. to
// point the Outlook definition's OAuth endpoints at a FakeMicrosoft server
// instead of the real internet, so the OAuth handshake journey (Slice 4) can
// run end to end through the real composition root.
func BootAppWithProviderDefinitions(t *testing.T, definitions []catalog.ProviderDefinition) *app.Wired {
	t.Helper()
	ctx := context.Background()

	wired, err := app.Wire(ctx, app.Deps{
		Config:              testConfig(t, NewTestDSN(t)),
		Logger:              testLogger(),
		ProviderDefinitions: definitions,
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// MovableClock is an injectable clock a test can advance without a real
// sleep (Slice 4): its Now method is app.Deps.Now, so a journey can travel
// time past a connect link's TTL, a connection's access-token expiry, or an
// api-key rotation's overlap window deterministically.
type MovableClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewMovableClock returns a MovableClock starting at start.
func NewMovableClock(start time.Time) *MovableClock {
	return &MovableClock{now: start}
}

// Now is app.Deps.Now's clock function.
func (c *MovableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *MovableClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// BootAppWithProviderDefinitionsAndClock is BootAppWithProviderDefinitions
// plus an injected clock override — Slice 4's token-refresh and reconnect
// journeys travel time (past ConnectLinkTTL, a connection's token_expires_at,
// etc.) without a real sleep.
func BootAppWithProviderDefinitionsAndClock(t *testing.T, definitions []catalog.ProviderDefinition, now func() time.Time) *app.Wired {
	t.Helper()
	ctx := context.Background()

	wired, err := app.Wire(ctx, app.Deps{
		Config:              testConfig(t, NewTestDSN(t)),
		Logger:              testLogger(),
		ProviderDefinitions: definitions,
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// BootAppWithProviderDefinitionsAndSleep is BootAppWithProviderDefinitions
// plus an injected retry-loop sleep override (PD21, Slice 6): rate-limit
// journeys substitute a SleepSpy (sleep_spy.go) so callWithRetry's
// Retry-After/jittered-backoff waits are captured and asserted on without a
// real delay.
func BootAppWithProviderDefinitionsAndSleep(t *testing.T, definitions []catalog.ProviderDefinition, sleep func(ctx context.Context, d time.Duration) error) *app.Wired {
	t.Helper()
	ctx := context.Background()

	wired, err := app.Wire(ctx, app.Deps{
		Config:              testConfig(t, NewTestDSN(t)),
		Logger:              testLogger(),
		ProviderDefinitions: definitions,
		Sleep:               sleep,
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// BootAppWithProviderDefinitionsClockAndSleep combines
// BootAppWithProviderDefinitionsAndClock and BootAppWithProviderDefinitionsAndSleep
// — the metrics journey needs both a movable clock (to force a token
// refresh) and a recording sleep (to keep a rate-limited retry fast and
// deterministic) in the same boot.
func BootAppWithProviderDefinitionsClockAndSleep(t *testing.T, definitions []catalog.ProviderDefinition, now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) *app.Wired {
	t.Helper()
	ctx := context.Background()

	wired, err := app.Wire(ctx, app.Deps{
		Config:              testConfig(t, NewTestDSN(t)),
		Logger:              testLogger(),
		ProviderDefinitions: definitions,
		Now:                 now,
		Sleep:               sleep,
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// BootAppWithProviderDefinitionsAndFileLimits is BootAppWithProviderDefinitions
// plus a configured files directory and max upload size (PD22, Slice 7): the
// oversize-rejection journey injects a small fileMaxBytes so it never has to
// allocate a real 20 MB payload just to exceed the limit.
func BootAppWithProviderDefinitionsAndFileLimits(t *testing.T, definitions []catalog.ProviderDefinition, filesDir string, fileMaxBytes int64) *app.Wired {
	t.Helper()
	ctx := context.Background()

	cfg := testConfig(t, NewTestDSN(t))
	cfg.FilesDir = filesDir
	cfg.FileMaxBytes = fileMaxBytes

	wired, err := app.Wire(ctx, app.Deps{
		Config:              cfg,
		Logger:              testLogger(),
		ProviderDefinitions: definitions,
	})
	if err != nil {
		t.Fatalf("app.Wire failed: %v", err)
	}
	t.Cleanup(func() { _ = wired.Close() })
	return wired
}

// testConfig is the PD12 config a test-booted app runs with: SQLite, the
// shared AdminAPIKey, a placeholder public base URL, and a files directory
// scoped to the calling test's own t.TempDir() (PD22, Slice 7) — so journeys
// that upload files clean up after themselves instead of accumulating in the
// shared os.TempDir()/beecon-files fallback. Callers that need a specific
// files directory/size limit (e.g. BootAppWithProviderDefinitionsAndFileLimits)
// overwrite these fields afterward.
func testConfig(t *testing.T, dsn string) *config.Config {
	t.Helper()
	return &config.Config{
		DatabaseDriver: config.DriverSQLite,
		DatabaseURL:    dsn,
		AdminAPIKey:    AdminAPIKey,
		BaseURL:        "http://localhost:8080",
		EncryptionKey:  EncryptionKeyBase64,
		FilesDir:       t.TempDir(),
	}
}

// testLogger discards log output so test runs stay quiet.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// sanitizeForDSN replaces characters t.Name() can contain (slashes from
// subtests, spaces) that are awkward in a DSN identifier.
func sanitizeForDSN(name string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return replacer.Replace(name)
}
