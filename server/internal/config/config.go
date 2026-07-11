// Package config reads Beecon's environment configuration (PD12): the
// database driver + URL, the installation admin key, the token-encryption
// key, and the public base URL. It fails fast with a clear message when
// required values are missing or malformed.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// EncryptionKeyBytes is the decoded byte length BEECON_ENCRYPTION_KEY must
// carry: an AES-256 key (PD12).
const EncryptionKeyBytes = 32

// DatabaseDriver is the persistence backend Beecon boots against.
type DatabaseDriver string

const (
	DriverPostgres DatabaseDriver = "postgres"
	DriverSQLite   DatabaseDriver = "sqlite"
)

// Config is Beecon's fully validated environment configuration.
type Config struct {
	DatabaseDriver DatabaseDriver
	DatabaseURL    string
	AdminAPIKey    string
	EncryptionKey  string
	BaseURL        string
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

	return &Config{
		DatabaseDriver: driver,
		DatabaseURL:    databaseURL,
		AdminAPIKey:    adminKey,
		EncryptionKey:  encryptionKey,
		BaseURL:        baseURL,
	}, nil
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
