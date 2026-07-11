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
	"sync/atomic"
	"testing"

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
		Config: testConfig(dsn),
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
		Config:              testConfig(NewTestDSN(t)),
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
// shared AdminAPIKey, and a placeholder public base URL.
func testConfig(dsn string) *config.Config {
	return &config.Config{
		DatabaseDriver: config.DriverSQLite,
		DatabaseURL:    dsn,
		AdminAPIKey:    AdminAPIKey,
		BaseURL:        "http://localhost:8080",
		EncryptionKey:  EncryptionKeyBase64,
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
