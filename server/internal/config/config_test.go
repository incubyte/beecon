package config_test

import (
	"strings"
	"testing"
	"time"

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

// --- BEECON_DELIVERY_TIMEOUT (PD29/PD30, Phase 3 Slice 3): unset falls
// back to DefaultDeliveryTimeoutSeconds (10s); a set value must parse as a
// positive integer number of seconds. ---

func TestLoad_UnsetDeliveryTimeoutFallsBackToTheDefaultTenSeconds(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DELIVERY_TIMEOUT", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDefault := config.DefaultDeliveryTimeoutSeconds * time.Second
	if cfg.DeliveryTimeout != wantDefault {
		t.Errorf("DeliveryTimeout = %v, want the default %v", cfg.DeliveryTimeout, wantDefault)
	}
	if config.DefaultDeliveryTimeoutSeconds != 10 {
		t.Fatalf("DefaultDeliveryTimeoutSeconds = %d, want exactly 10", config.DefaultDeliveryTimeoutSeconds)
	}
}

func TestLoad_ASetDeliveryTimeoutValueIsUsedAsSeconds(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DELIVERY_TIMEOUT", "30")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DeliveryTimeout != 30*time.Second {
		t.Errorf("DeliveryTimeout = %v, want %v", cfg.DeliveryTimeout, 30*time.Second)
	}
}

func TestLoad_NonIntegerDeliveryTimeoutFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DELIVERY_TIMEOUT", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_DELIVERY_TIMEOUT, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_DELIVERY_TIMEOUT") {
		t.Errorf("error = %q, want it to name BEECON_DELIVERY_TIMEOUT", err.Error())
	}
}

func TestLoad_NonPositiveDeliveryTimeoutFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_DELIVERY_TIMEOUT", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_DELIVERY_TIMEOUT=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_DELIVERY_TIMEOUT") {
				t.Errorf("error = %q, want it to name BEECON_DELIVERY_TIMEOUT", err.Error())
			}
		})
	}
}

func TestLoad_WhitespaceOnlyDeliveryTimeoutIsTreatedAsUnsetAndFallsBackToTheDefault(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_DELIVERY_TIMEOUT", "   ")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DeliveryTimeout != config.DefaultDeliveryTimeoutSeconds*time.Second {
		t.Errorf("DeliveryTimeout = %v, want the default", cfg.DeliveryTimeout)
	}
}

// --- BEECON_POLL_MIN_INTERVAL (PD28, Phase 3 Slice 4): unset falls back to
// DefaultPollMinIntervalSeconds (30s); a set value must parse as a positive
// integer number of seconds. ---

func TestLoad_UnsetPollMinIntervalFallsBackToTheDefaultThirtySeconds(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_POLL_MIN_INTERVAL", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDefault := config.DefaultPollMinIntervalSeconds * time.Second
	if cfg.PollMinInterval != wantDefault {
		t.Errorf("PollMinInterval = %v, want the default %v", cfg.PollMinInterval, wantDefault)
	}
	if config.DefaultPollMinIntervalSeconds != 30 {
		t.Fatalf("DefaultPollMinIntervalSeconds = %d, want exactly 30", config.DefaultPollMinIntervalSeconds)
	}
}

func TestLoad_ASetPollMinIntervalValueIsUsedAsSeconds(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_POLL_MIN_INTERVAL", "45")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PollMinInterval != 45*time.Second {
		t.Errorf("PollMinInterval = %v, want %v", cfg.PollMinInterval, 45*time.Second)
	}
}

func TestLoad_NonIntegerPollMinIntervalFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_POLL_MIN_INTERVAL", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_POLL_MIN_INTERVAL, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_POLL_MIN_INTERVAL") {
		t.Errorf("error = %q, want it to name BEECON_POLL_MIN_INTERVAL", err.Error())
	}
}

func TestLoad_NonPositivePollMinIntervalFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_POLL_MIN_INTERVAL", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_POLL_MIN_INTERVAL=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_POLL_MIN_INTERVAL") {
				t.Errorf("error = %q, want it to name BEECON_POLL_MIN_INTERVAL", err.Error())
			}
		})
	}
}

func TestLoad_WhitespaceOnlyPollMinIntervalIsTreatedAsUnsetAndFallsBackToTheDefault(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_POLL_MIN_INTERVAL", "   ")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PollMinInterval != config.DefaultPollMinIntervalSeconds*time.Second {
		t.Errorf("PollMinInterval = %v, want the default", cfg.PollMinInterval)
	}
}

// --- BEECON_RETENTION_DAYS / BEECON_PURGE_INTERVAL (PD44, Phase 4 Slice 7) ---

func TestLoad_UnsetRetentionDaysFallsBackToTheDefaultThirtyDays(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_RETENTION_DAYS", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RetentionDays != config.DefaultRetentionDays {
		t.Errorf("RetentionDays = %d, want the default %d", cfg.RetentionDays, config.DefaultRetentionDays)
	}
	if config.DefaultRetentionDays != 30 {
		t.Fatalf("DefaultRetentionDays = %d, want exactly 30", config.DefaultRetentionDays)
	}
}

func TestLoad_ASetRetentionDaysValueIsUsedVerbatim(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_RETENTION_DAYS", "90")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RetentionDays != 90 {
		t.Errorf("RetentionDays = %d, want 90", cfg.RetentionDays)
	}
}

func TestLoad_NonIntegerRetentionDaysFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_RETENTION_DAYS", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_RETENTION_DAYS, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_RETENTION_DAYS") {
		t.Errorf("error = %q, want it to name BEECON_RETENTION_DAYS", err.Error())
	}
}

// TestLoad_NonPositiveRetentionDaysFailsFastWithClearMessage pins that the
// installation-wide default itself may never be 0/unlimited — only a
// per-org override on org_governance may be — even though 0 is a meaningful
// value elsewhere in this same feature (PD44).
func TestLoad_NonPositiveRetentionDaysFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_RETENTION_DAYS", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_RETENTION_DAYS=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_RETENTION_DAYS") {
				t.Errorf("error = %q, want it to name BEECON_RETENTION_DAYS", err.Error())
			}
		})
	}
}

func TestLoad_UnsetPurgeIntervalFallsBackToTheDefaultOneDay(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_PURGE_INTERVAL", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDefault := config.DefaultPurgeIntervalSeconds * time.Second
	if cfg.PurgeInterval != wantDefault {
		t.Errorf("PurgeInterval = %v, want the default %v", cfg.PurgeInterval, wantDefault)
	}
	if config.DefaultPurgeIntervalSeconds != 24*60*60 {
		t.Fatalf("DefaultPurgeIntervalSeconds = %d, want exactly 24h in seconds", config.DefaultPurgeIntervalSeconds)
	}
}

func TestLoad_ASetPurgeIntervalValueIsUsedAsSeconds(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_PURGE_INTERVAL", "3600")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PurgeInterval != time.Hour {
		t.Errorf("PurgeInterval = %v, want %v", cfg.PurgeInterval, time.Hour)
	}
}

func TestLoad_NonPositivePurgeIntervalFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_PURGE_INTERVAL", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_PURGE_INTERVAL=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_PURGE_INTERVAL") {
				t.Errorf("error = %q, want it to name BEECON_PURGE_INTERVAL", err.Error())
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

// --- BEECON_WEBHOOK_ENDPOINT_CAP / BEECON_ENDPOINT_AUTODISABLE_FAILURES
// (PD45, Phase 4 Slice 8) — both share parsePositiveIntSetting's shape. ---

func TestLoad_UnsetWebhookEndpointCapFallsBackToTheDefaultFive(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_WEBHOOK_ENDPOINT_CAP", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebhookEndpointCap != config.DefaultWebhookEndpointCap {
		t.Errorf("WebhookEndpointCap = %d, want the default %d", cfg.WebhookEndpointCap, config.DefaultWebhookEndpointCap)
	}
	if config.DefaultWebhookEndpointCap != 5 {
		t.Fatalf("DefaultWebhookEndpointCap = %d, want exactly 5", config.DefaultWebhookEndpointCap)
	}
}

func TestLoad_ASetWebhookEndpointCapValueIsUsedVerbatim(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_WEBHOOK_ENDPOINT_CAP", "10")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WebhookEndpointCap != 10 {
		t.Errorf("WebhookEndpointCap = %d, want 10", cfg.WebhookEndpointCap)
	}
}

func TestLoad_NonIntegerWebhookEndpointCapFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_WEBHOOK_ENDPOINT_CAP", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_WEBHOOK_ENDPOINT_CAP, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_WEBHOOK_ENDPOINT_CAP") {
		t.Errorf("error = %q, want it to name BEECON_WEBHOOK_ENDPOINT_CAP", err.Error())
	}
}

func TestLoad_NonPositiveWebhookEndpointCapFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_WEBHOOK_ENDPOINT_CAP", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_WEBHOOK_ENDPOINT_CAP=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_WEBHOOK_ENDPOINT_CAP") {
				t.Errorf("error = %q, want it to name BEECON_WEBHOOK_ENDPOINT_CAP", err.Error())
			}
		})
	}
}

func TestLoad_UnsetEndpointAutoDisableFailuresFallsBackToTheDefaultFive(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_ENDPOINT_AUTODISABLE_FAILURES", "")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EndpointAutoDisableFailures != config.DefaultEndpointAutoDisableFailures {
		t.Errorf("EndpointAutoDisableFailures = %d, want the default %d", cfg.EndpointAutoDisableFailures, config.DefaultEndpointAutoDisableFailures)
	}
	if config.DefaultEndpointAutoDisableFailures != 5 {
		t.Fatalf("DefaultEndpointAutoDisableFailures = %d, want exactly 5", config.DefaultEndpointAutoDisableFailures)
	}
}

func TestLoad_ASetEndpointAutoDisableFailuresValueIsUsedVerbatim(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_ENDPOINT_AUTODISABLE_FAILURES", "3")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EndpointAutoDisableFailures != 3 {
		t.Errorf("EndpointAutoDisableFailures = %d, want 3", cfg.EndpointAutoDisableFailures)
	}
}

func TestLoad_NonIntegerEndpointAutoDisableFailuresFailsFastWithClearMessage(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_ENDPOINT_AUTODISABLE_FAILURES", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected an error for a non-integer BEECON_ENDPOINT_AUTODISABLE_FAILURES, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_ENDPOINT_AUTODISABLE_FAILURES") {
		t.Errorf("error = %q, want it to name BEECON_ENDPOINT_AUTODISABLE_FAILURES", err.Error())
	}
}

func TestLoad_NonPositiveEndpointAutoDisableFailuresFailsFastWithClearMessage(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		t.Run(value, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("BEECON_ENDPOINT_AUTODISABLE_FAILURES", value)

			_, err := config.Load()

			if err == nil {
				t.Fatalf("expected an error for BEECON_ENDPOINT_AUTODISABLE_FAILURES=%q, got nil", value)
			}
			if !strings.Contains(err.Error(), "BEECON_ENDPOINT_AUTODISABLE_FAILURES") {
				t.Errorf("error = %q, want it to name BEECON_ENDPOINT_AUTODISABLE_FAILURES", err.Error())
			}
		})
	}
}
