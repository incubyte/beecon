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

// --- DecodeEncryptionKey (AC11: boot fails fast, naming BEECON_ENCRYPTION_KEY,
// for a missing, non-base64, or wrong-length token encryption key). ---

// validEncryptionKeyBase64 base64-encodes exactly 32 bytes.
const validEncryptionKeyBase64 = "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="

func TestDecodeEncryptionKey_ReturnsTheDecoded32ByteKeyForAValidValue(t *testing.T) {
	key, err := config.DecodeEncryptionKey(validEncryptionKeyBase64)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != config.EncryptionKeyBytes {
		t.Errorf("len(key) = %d, want %d", len(key), config.EncryptionKeyBytes)
	}
}

func TestDecodeEncryptionKey_FailsNamingTheVariableWhenMissing(t *testing.T) {
	_, err := config.DecodeEncryptionKey("")

	if err == nil {
		t.Fatal("expected an error for a missing encryption key, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}

func TestDecodeEncryptionKey_FailsNamingTheVariableWhenNotValidBase64(t *testing.T) {
	_, err := config.DecodeEncryptionKey("not-valid-base64!!!")

	if err == nil {
		t.Fatal("expected an error for a non-base64 encryption key, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}

func TestDecodeEncryptionKey_FailsNamingTheVariableWhenDecodedLengthIsTooShort(t *testing.T) {
	// base64 of 16 bytes, not 32.
	_, err := config.DecodeEncryptionKey("MDEyMzQ1Njc4OTAxMjM0NQ==")

	if err == nil {
		t.Fatal("expected an error for a too-short encryption key, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}

// --- BEECON_FILE_MAX_BYTES (PD22, Slice 7, AC3): unset falls back to
// DefaultFileMaxBytes (20 MB); a set value must parse as a positive
// integer. ---

func TestLoad_UnsetFileMaxBytesFallsBackToTheDefault20MB(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_FILE_MAX_BYTES", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FileMaxBytes != config.DefaultFileMaxBytes {
		t.Errorf("FileMaxBytes = %d, want the default %d (20 MB)", cfg.FileMaxBytes, config.DefaultFileMaxBytes)
	}
	if config.DefaultFileMaxBytes != 20*1024*1024 {
		t.Fatalf("DefaultFileMaxBytes = %d, want exactly 20 MB (20*1024*1024)", config.DefaultFileMaxBytes)
	}
}

func TestLoad_ASetFileMaxBytesValueIsUsedVerbatim(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_FILE_MAX_BYTES", "5242880")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FileMaxBytes != 5242880 {
		t.Errorf("FileMaxBytes = %d, want %d", cfg.FileMaxBytes, 5242880)
	}
}

func TestLoad_NonIntegerFileMaxBytesFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_FILE_MAX_BYTES", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_FILE_MAX_BYTES, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_FILE_MAX_BYTES") {
		t.Errorf("error = %q, want it to name BEECON_FILE_MAX_BYTES", err.Error())
	}
}

func TestLoad_NonPositiveFileMaxBytesFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_FILE_MAX_BYTES", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_FILE_MAX_BYTES=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_FILE_MAX_BYTES") {
				t.Errorf("error = %q, want it to name BEECON_FILE_MAX_BYTES", err.Error())
			}
		})
	}
}

func TestDecodeEncryptionKey_FailsNamingTheVariableWhenDecodedLengthIsTooLong(t *testing.T) {
	// base64 of 48 bytes, not 32.
	_, err := config.DecodeEncryptionKey("MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDEyMzQ1Njc4OTAxMjM0NTY3")

	if err == nil {
		t.Fatal("expected an error for a too-long encryption key, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ENCRYPTION_KEY") {
		t.Errorf("error = %q, want it to name BEECON_ENCRYPTION_KEY", err.Error())
	}
}
