// Package config_test — this file covers Phase 5 Slice 1's new config
// surfaces (PD49/PD52): BEECON_SESSION_TTL's fail-fast parsing/default, and
// SecureCookies' derivation from BEECON_BASE_URL's scheme (FD-E) — never a
// separate config knob, so the operator-console cookies' Secure flag can't
// drift out of sync with the installation's actual public scheme.
package config_test

import (
	"testing"
	"time"

	"beecon/internal/config"
)

func TestLoad_SessionTTLDefaultsToTwelveHoursWhenUnset(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionTTL != 12*time.Hour {
		t.Errorf("SessionTTL = %v, want %v", cfg.SessionTTL, 12*time.Hour)
	}
}

func TestLoad_SessionTTLHonorsAnExplicitSecondsValue(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_SESSION_TTL", "3600")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionTTL != time.Hour {
		t.Errorf("SessionTTL = %v, want %v", cfg.SessionTTL, time.Hour)
	}
}

func TestLoad_FailsFastOnANonNumericSessionTTL(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_SESSION_TTL", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a non-numeric BEECON_SESSION_TTL")
	}
}

func TestSecureCookies_TrueForAnHttpsBaseURL(t *testing.T) {
	if !config.SecureCookies("https://console.example.com") {
		t.Error("expected SecureCookies to be true for an https:// base URL")
	}
}

func TestSecureCookies_FalseForAnHttpBaseURL(t *testing.T) {
	if config.SecureCookies("http://localhost:8080") {
		t.Error("expected SecureCookies to be false for a plain http:// base URL (local/dev, FD-E)")
	}
}

func TestSecureCookies_FalseForAnEmptyBaseURL(t *testing.T) {
	if config.SecureCookies("") {
		t.Error("expected SecureCookies to be false for an empty base URL rather than panicking or defaulting true")
	}
}
