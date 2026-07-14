// Package config reads Beecon's environment configuration (PD12): the
// database driver + URL, the installation admin key, the token-encryption
// key, and the public base URL. It fails fast with a clear message when
// required values are missing or malformed.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// EncryptionKeyBytes is the decoded byte length BEECON_ENCRYPTION_KEY must
// carry: an AES-256 key (PD12).
const EncryptionKeyBytes = 32

// DefaultFileMaxBytes is BEECON_FILE_MAX_BYTES's fallback when unset (PD22,
// Slice 7, AC3): 20 MB.
const DefaultFileMaxBytes int64 = 20 * 1024 * 1024

// DefaultDeliveryTimeoutSeconds is BEECON_DELIVERY_TIMEOUT's fallback when
// unset (PD29/PD30, Phase 3 Slice 3): how many seconds DispatchOnce waits
// for a consumer's webhook endpoint to answer before treating the attempt
// as failed.
const DefaultDeliveryTimeoutSeconds = 10

// DefaultPollMinIntervalSeconds is BEECON_POLL_MIN_INTERVAL's fallback when
// unset (PD28, Phase 3 Slice 4): the floor triggers.Facade.PollOnce applies
// to every instance's own poll interval, mirroring catalog's own boot-time
// clamp default (definition_v1.go's platformMinPollIntervalSeconds).
const DefaultPollMinIntervalSeconds = 30

// DefaultRefreshLeadSeconds is BEECON_REFRESH_LEAD's fallback when unset
// (PD36, Phase 3 Slice 5): connections.Facade.RefreshDueOnce claims ACTIVE
// connections whose access token expires within this many seconds — 10
// minutes.
const DefaultRefreshLeadSeconds = 10 * 60

// DefaultRefreshScanIntervalSeconds is BEECON_REFRESH_SCAN_INTERVAL's
// fallback when unset (PD36): how often the refresh worker loop itself ticks
// — 60 seconds.
const DefaultRefreshScanIntervalSeconds = 60

// DefaultReconcileIntervalSeconds is BEECON_RECONCILE_INTERVAL's fallback
// when unset (PD37): how often connections.Facade.ReconcileOnce re-verifies
// an ACTIVE connection against its provider, and how often the
// reconciliation worker loop itself ticks — 6 hours.
const DefaultReconcileIntervalSeconds = 6 * 60 * 60

// DatabaseDriver is the persistence backend Beecon boots against.
type DatabaseDriver string

const (
	DriverPostgres DatabaseDriver = "postgres"
	DriverSQLite   DatabaseDriver = "sqlite"
)

// Config is Beecon's fully validated environment configuration.
type Config struct {
	DatabaseDriver      DatabaseDriver
	DatabaseURL         string
	AdminAPIKey         string
	EncryptionKey       string
	BaseURL             string
	FilesDir            string
	FileMaxBytes        int64
	DeliveryTimeout     time.Duration
	PollMinInterval     time.Duration
	RefreshLead         time.Duration
	RefreshScanInterval time.Duration
	ReconcileInterval   time.Duration
}

// Load reads .env.local (if present) then the process environment, and
// validates the PD12 surface. It never mutates the process environment.
func Load() (*Config, error) {
	env := loadEnv()

	driver, err := parseDatabaseDriver(env("BEECON_DATABASE_DRIVER"))
	if err != nil {
		return nil, err
	}

	databaseURL := strings.TrimSpace(env("BEECON_DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("BEECON_DATABASE_URL is not set")
	}

	adminKey := strings.TrimSpace(env("BEECON_ADMIN_API_KEY"))
	if adminKey == "" {
		return nil, fmt.Errorf("BEECON_ADMIN_API_KEY is not set")
	}

	baseURL := strings.TrimSpace(env("BEECON_BASE_URL"))
	if baseURL == "" {
		return nil, fmt.Errorf("BEECON_BASE_URL is not set")
	}

	// BEECON_ENCRYPTION_KEY is validated (32-byte, base64) starting with
	// Slice 4, where token encryption is introduced; Slice 1 only carries the
	// raw value through.
	encryptionKey := strings.TrimSpace(env("BEECON_ENCRYPTION_KEY"))

	// BEECON_FILES_DIR (PD22, Slice 7) is carried through unvalidated here —
	// the same pattern as BEECON_ENCRYPTION_KEY: filestore.NewLocal validates
	// it at boot (wiring.go), where a missing value names the exact problem.
	filesDir := strings.TrimSpace(env("BEECON_FILES_DIR"))

	fileMaxBytes, err := parseFileMaxBytes(env("BEECON_FILE_MAX_BYTES"))
	if err != nil {
		return nil, err
	}

	deliveryTimeout, err := parseDeliveryTimeoutSeconds(env("BEECON_DELIVERY_TIMEOUT"))
	if err != nil {
		return nil, err
	}

	pollMinInterval, err := parsePollMinIntervalSeconds(env("BEECON_POLL_MIN_INTERVAL"))
	if err != nil {
		return nil, err
	}

	refreshLead, err := parseSecondsSetting("BEECON_REFRESH_LEAD", env("BEECON_REFRESH_LEAD"), DefaultRefreshLeadSeconds)
	if err != nil {
		return nil, err
	}

	refreshScanInterval, err := parseSecondsSetting("BEECON_REFRESH_SCAN_INTERVAL", env("BEECON_REFRESH_SCAN_INTERVAL"), DefaultRefreshScanIntervalSeconds)
	if err != nil {
		return nil, err
	}

	reconcileInterval, err := parseSecondsSetting("BEECON_RECONCILE_INTERVAL", env("BEECON_RECONCILE_INTERVAL"), DefaultReconcileIntervalSeconds)
	if err != nil {
		return nil, err
	}

	return &Config{
		DatabaseDriver:      driver,
		DatabaseURL:         databaseURL,
		AdminAPIKey:         adminKey,
		EncryptionKey:       encryptionKey,
		BaseURL:             baseURL,
		FilesDir:            filesDir,
		FileMaxBytes:        fileMaxBytes,
		DeliveryTimeout:     deliveryTimeout,
		PollMinInterval:     pollMinInterval,
		RefreshLead:         refreshLead,
		RefreshScanInterval: refreshScanInterval,
		ReconcileInterval:   reconcileInterval,
	}, nil
}

// parseFileMaxBytes reads BEECON_FILE_MAX_BYTES (PD22, AC3): unset falls
// back to DefaultFileMaxBytes (20 MB); set, it must parse as a positive
// integer.
func parseFileMaxBytes(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultFileMaxBytes, nil
	}
	parsed, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("BEECON_FILE_MAX_BYTES must be a positive integer, got %q", raw)
	}
	return parsed, nil
}

// parseDeliveryTimeoutSeconds reads BEECON_DELIVERY_TIMEOUT (PD29/PD30):
// unset falls back to DefaultDeliveryTimeoutSeconds (10s); set, it must
// parse as a positive integer number of seconds.
func parseDeliveryTimeoutSeconds(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultDeliveryTimeoutSeconds * time.Second, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("BEECON_DELIVERY_TIMEOUT must be a positive integer number of seconds, got %q", raw)
	}
	return time.Duration(parsed) * time.Second, nil
}

// parsePollMinIntervalSeconds reads BEECON_POLL_MIN_INTERVAL (PD28, Phase 3
// Slice 4): unset falls back to DefaultPollMinIntervalSeconds (30s); set, it
// must parse as a positive integer number of seconds.
func parsePollMinIntervalSeconds(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultPollMinIntervalSeconds * time.Second, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("BEECON_POLL_MIN_INTERVAL must be a positive integer number of seconds, got %q", raw)
	}
	return time.Duration(parsed) * time.Second, nil
}

// parseSecondsSetting reads a BEECON_* environment variable expressed as a
// positive integer number of seconds (PD36/PD37's BEECON_REFRESH_LEAD,
// BEECON_REFRESH_SCAN_INTERVAL, and BEECON_RECONCILE_INTERVAL all share this
// shape): unset falls back to defaultSeconds; set, it must parse as a
// positive integer. name is only used to name the problem in the returned
// error.
func parseSecondsSetting(name, raw string, defaultSeconds int) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Duration(defaultSeconds) * time.Second, nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of seconds, got %q", name, raw)
	}
	return time.Duration(parsed) * time.Second, nil
}

// loadEnv loads .env.local into the process environment (without overriding
// values already set) and returns a lookup func over os.Getenv.
func loadEnv() func(string) string {
	_ = godotenv.Load(".env.local")
	return os.Getenv
}

// DecodeEncryptionKey validates raw as PD12's BEECON_ENCRYPTION_KEY: present,
// valid base64, decoding to exactly EncryptionKeyBytes (AES-256-GCM's key
// size). Wire calls this at boot, after the database connects, so it fails
// fast with a clear message naming the problem (AC11) without disturbing the
// Slice 1 config surface that carries EncryptionKey through unvalidated for
// callers that don't need token encryption yet.
func DecodeEncryptionKey(raw string) ([]byte, error) {
	if raw == "" {
		return nil, fmt.Errorf("BEECON_ENCRYPTION_KEY is not set")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("BEECON_ENCRYPTION_KEY must be valid base64: %w", err)
	}
	if len(key) != EncryptionKeyBytes {
		return nil, fmt.Errorf("BEECON_ENCRYPTION_KEY must decode to exactly %d bytes, got %d", EncryptionKeyBytes, len(key))
	}
	return key, nil
}

func parseDatabaseDriver(raw string) (DatabaseDriver, error) {
	switch DatabaseDriver(strings.TrimSpace(raw)) {
	case DriverPostgres:
		return DriverPostgres, nil
	case DriverSQLite:
		return DriverSQLite, nil
	default:
		return "", fmt.Errorf("BEECON_DATABASE_DRIVER must be %q or %q, got %q", DriverPostgres, DriverSQLite, raw)
	}
}
