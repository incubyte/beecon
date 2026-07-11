package app

import (
	"context"
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
