package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"beecon/internal/config"
)

// TestWire_FailsWithAClearMessageWhenTheDatabaseIsUnreachable points Wire at
// a postgres DSN with nothing listening on the port: the connection attempt
// to localhost is refused immediately (no retries/backoff involved), so this
// stays well under the 2s context deadline used as a safety net.
func TestWire_FailsWithAClearMessageWhenTheDatabaseIsUnreachable(t *testing.T) {
	cfg := &config.Config{
		DatabaseDriver: config.DriverPostgres,
		DatabaseURL:    "postgres://user:pass@127.0.0.1:1/nonexistent?sslmode=disable",
		AdminAPIKey:    "test-admin-key",
		BaseURL:        "http://localhost:8080",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := Wire(ctx, Deps{Config: cfg, Logger: logger})

	if err == nil {
		t.Fatal("expected Wire to fail against an unreachable database, got nil error")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "unreachable")
	}
}

// sqliteTestConfig is a PD12 config that gets Wire past the database
// connect/migrate steps (a fresh, uniquely named SQLite in-memory database),
// so these tests exercise Wire's own encryption-key validation (AC11) rather
// than failing earlier at "database unreachable".
func sqliteTestConfig(t *testing.T, encryptionKey string) *config.Config {
	t.Helper()
	dsn := fmt.Sprintf("file:wiring_test_%s?mode=memory&cache=shared", sanitizeDSNName(t.Name()))
	return &config.Config{
		DatabaseDriver: config.DriverSQLite,
		DatabaseURL:    dsn,
		AdminAPIKey:    "test-admin-key",
		BaseURL:        "http://localhost:8080",
		EncryptionKey:  encryptionKey,
	}
}

func sanitizeDSNName(name string) string {
	replaced := make([]rune, 0, len(name))
	for _, r := range name {
		if r == '/' || r == ' ' {
			replaced = append(replaced, '_')
			continue
		}
		replaced = append(replaced, r)
	}
	return string(replaced)
}

// TestWire_FailsFastWhenTheEncryptionKeyIsMissing is AC11 (missing variant):
// boot fails with a message naming BEECON_ENCRYPTION_KEY rather than
// constructing a Vault it cannot use.
func TestWire_FailsFastWhenTheEncryptionKeyIsMissing(t *testing.T) {
	cfg := sqliteTestConfig(t, "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := Wire(context.Background(), Deps{Config: cfg, Logger: logger})

	if err == nil {
		t.Fatal("expected Wire to fail with a missing encryption key, got nil error")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}

// TestWire_FailsFastWhenTheEncryptionKeyIsNotValidBase64 is AC11 (malformed
// variant).
func TestWire_FailsFastWhenTheEncryptionKeyIsNotValidBase64(t *testing.T) {
	cfg := sqliteTestConfig(t, "not-valid-base64!!!")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := Wire(context.Background(), Deps{Config: cfg, Logger: logger})

	if err == nil {
		t.Fatal("expected Wire to fail with a malformed encryption key, got nil error")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}

// TestWire_FailsFastWhenTheEncryptionKeyIsTheWrongLength is AC11 (wrong-length
// variant): valid base64, but not 32 bytes once decoded.
func TestWire_FailsFastWhenTheEncryptionKeyIsTheWrongLength(t *testing.T) {
	// base64 of 16 bytes, not the required 32.
	cfg := sqliteTestConfig(t, "MDEyMzQ1Njc4OTAxMjM0NQ==")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := Wire(context.Background(), Deps{Config: cfg, Logger: logger})

	if err == nil {
		t.Fatal("expected Wire to fail with a wrong-length encryption key, got nil error")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}
