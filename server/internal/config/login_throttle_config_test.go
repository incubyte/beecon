// Package config_test — this file covers Phase 5 Slice 5's new config
// surfaces (FD-G): BEECON_LOGIN_MAX_ATTEMPTS and BEECON_LOGIN_LOCKOUT's
// fail-fast parsing/defaults, mirroring operator_auth_config_test.go's own
// convention for BEECON_SESSION_TTL (setValidEnv, defined in config_test.go,
// is reused here).
package config_test

import (
	"testing"
	"time"

	"beecon/internal/config"
)

// --- BEECON_LOGIN_MAX_ATTEMPTS. ---

func TestLoad_LoginMaxAttemptsDefaultsToFiveWhenUnset(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LoginMaxAttempts != 5 {
		t.Errorf("LoginMaxAttempts = %d, want 5", cfg.LoginMaxAttempts)
	}
}

func TestLoad_LoginMaxAttemptsHonorsAnExplicitValue(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_MAX_ATTEMPTS", "10")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LoginMaxAttempts != 10 {
		t.Errorf("LoginMaxAttempts = %d, want 10", cfg.LoginMaxAttempts)
	}
}

func TestLoad_FailsFastOnANonIntegerLoginMaxAttempts(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_MAX_ATTEMPTS", "not-a-number")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a non-integer BEECON_LOGIN_MAX_ATTEMPTS")
	}
}

func TestLoad_FailsFastOnAZeroLoginMaxAttempts(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_MAX_ATTEMPTS", "0")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a zero BEECON_LOGIN_MAX_ATTEMPTS (a threshold of zero would lock out the first attempt on every account)")
	}
}

func TestLoad_FailsFastOnANegativeLoginMaxAttempts(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_MAX_ATTEMPTS", "-1")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a negative BEECON_LOGIN_MAX_ATTEMPTS")
	}
}

// --- BEECON_LOGIN_LOCKOUT. ---

func TestLoad_LoginLockoutDefaultsToFifteenMinutesWhenUnset(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LoginLockout != 15*time.Minute {
		t.Errorf("LoginLockout = %v, want %v", cfg.LoginLockout, 15*time.Minute)
	}
}

func TestLoad_LoginLockoutHonorsAnExplicitSecondsValue(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_LOCKOUT", "60")

	cfg, err := config.Load()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LoginLockout != time.Minute {
		t.Errorf("LoginLockout = %v, want %v", cfg.LoginLockout, time.Minute)
	}
}

func TestLoad_FailsFastOnANonIntegerLoginLockout(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_LOCKOUT", "fifteen minutes")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a non-numeric BEECON_LOGIN_LOCKOUT")
	}
}

func TestLoad_FailsFastOnAZeroLoginLockout(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_LOCKOUT", "0")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a zero BEECON_LOGIN_LOCKOUT (no cooldown at all is not a valid lockout window)")
	}
}

func TestLoad_FailsFastOnANegativeLoginLockout(t *testing.T) {
	setValidEnv(t)
	t.Setenv("BEECON_LOGIN_LOCKOUT", "-900")

	_, err := config.Load()

	if err == nil {
		t.Fatal("expected Load to fail fast on a negative BEECON_LOGIN_LOCKOUT")
	}
}
