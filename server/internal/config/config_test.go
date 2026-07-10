package config_test

import (
	"strings"
	"testing"

	"beecon/internal/config"
)

// setValidEnv sets every PD12-required env var to a valid value and returns a
// cleanup-free setup (t.Setenv already restores previous values after the
// test). Individual tests overwrite the variable they want to break.
func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BEECON_DATABASE_DRIVER", "sqlite")
	t.Setenv("BEECON_DATABASE_URL", "file:test?mode=memory")
	t.Setenv("BEECON_ADMIN_API_KEY", "test-admin-key")
	t.Setenv("BEECON_BASE_URL", "http://localhost:8080")
	t.Setenv("BEECON_ENCRYPTION_KEY", "")
}

func TestLoad_ValidEnvironmentReturnsConfig(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseDriver != config.DriverSQLite {
		t.Errorf("DatabaseDriver = %q, want %q", cfg.DatabaseDriver, config.DriverSQLite)
	}
	if cfg.DatabaseURL != "file:test?mode=memory" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "file:test?mode=memory")
	}
	if cfg.AdminAPIKey != "test-admin-key" {
		t.Errorf("AdminAPIKey = %q, want %q", cfg.AdminAPIKey, "test-admin-key")
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "http://localhost:8080")
	}
}

func TestLoad_AcceptsPostgresDriver(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DATABASE_DRIVER", "postgres")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DatabaseDriver != config.DriverPostgres {
		t.Errorf("DatabaseDriver = %q, want %q", cfg.DatabaseDriver, config.DriverPostgres)
	}
}

func TestLoad_MissingDatabaseDriverFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DATABASE_DRIVER", "")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for missing BEECON_DATABASE_DRIVER, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_DATABASE_DRIVER") {
		t.Errorf("error = %q, want it to name BEECON_DATABASE_DRIVER", err.Error())
	}
}

func TestLoad_InvalidDatabaseDriverValueFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DATABASE_DRIVER", "mysql")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for an invalid BEECON_DATABASE_DRIVER value, got nil")
	}
	if !strings.Contains(err.Error(), "mysql") {
		t.Errorf("error = %q, want it to echo the invalid value %q", err.Error(), "mysql")
	}
}

func TestLoad_MissingDatabaseURLFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DATABASE_URL", "")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for missing BEECON_DATABASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_DATABASE_URL") {
		t.Errorf("error = %q, want it to name BEECON_DATABASE_URL", err.Error())
	}
}

func TestLoad_BlankDatabaseURLIsTreatedAsMissing(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DATABASE_URL", "   ")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected a whitespace-only BEECON_DATABASE_URL to fail validation")
	}
}

func TestLoad_MissingAdminAPIKeyFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_ADMIN_API_KEY", "")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for missing BEECON_ADMIN_API_KEY, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ADMIN_API_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ADMIN_API_KEY", err.Error())
	}
}

func TestLoad_MissingBaseURLFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_BASE_URL", "")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for missing BEECON_BASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_BASE_URL") {
		t.Errorf("error = %q, want it to name BEECON_BASE_URL", err.Error())
	}
}
