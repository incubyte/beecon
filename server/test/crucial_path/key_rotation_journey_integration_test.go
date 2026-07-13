//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// wireErrorEnvelope, and doJSONRequest already declared there). This file
// tells Slice 8/PD23's story end to end against the real composition root:
// an installation admin rotates an organization's api key -> the new secret
// authenticates immediately, the old secret keeps authenticating through the
// (default and a custom) overlap window, clock travel past that window
// rejects the old secret, listing shows rotation state and never a secret,
// and revoking a rotated key immediately kills both the old and the new
// secret. The new secret is asserted to appear in no response but the rotate
// response itself, and the database dump behind it holds only hashes.
package crucial_path

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/test/support"
)

type rotatedKeyDTO struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Prefix           string `json:"prefix"`
	OverlapExpiresAt string `json:"overlapExpiresAt"`
}

type keyDTO struct {
	ID               string  `json:"id"`
	Prefix           string  `json:"prefix"`
	CreatedAt        string  `json:"createdAt"`
	RotatedAt        *string `json:"rotatedAt"`
	OverlapExpiresAt *string `json:"overlapExpiresAt"`
}

// createOrgAndKey creates a fresh organization and issues its first server
// api key through the admin key, returning both the org and the issued key.
func createOrgAndKey(t *testing.T, router http.Handler, adminAuth, orgName string) (organizationDTO, issuedKeyDTO) {
	t.Helper()
	var org organizationDTO
	w := doJSONRequest(t, router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"`+orgName+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var issued issuedKeyDTO
	w = doJSONRequest(t, router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &issued); err != nil {
		t.Fatalf("decode issued key: %v", err)
	}
	return org, issued
}

func TestKeyRotationJourney_NewKeyWorksImmediatelyOldKeyWorksInsideWindowClockTravelPastWindowRevokeKillsBoth(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, nil, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey

	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Rotation Co")
	originalAuth := "Bearer " + issued.Key

	t.Run("the original secret authenticates before any rotation", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Pre-Rotation User"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	var rotated rotatedKeyDTO
	t.Run("rotating the key (AC1) returns 201 with a fresh secret different from the original", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys/"+issued.ID+"/rotate", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
			t.Fatalf("decode rotate response: %v; body=%s", err, w.Body.String())
		}
		if rotated.ID != issued.ID {
			t.Errorf("id = %q, want the same key id %q", rotated.ID, issued.ID)
		}
		if rotated.Key == "" || rotated.Key == issued.Key {
			t.Fatalf("key = %q, want a fresh secret distinct from the original %q", rotated.Key, issued.Key)
		}
		wantOverlapExpiresAt := clock.Now().Add(24 * time.Hour)
		gotOverlapExpiresAt, err := time.Parse(rfc3339MillisLayout, rotated.OverlapExpiresAt)
		if err != nil {
			t.Fatalf("parse overlapExpiresAt %q: %v", rotated.OverlapExpiresAt, err)
		}
		if !gotOverlapExpiresAt.Equal(wantOverlapExpiresAt) {
			t.Errorf("overlapExpiresAt = %v, want %v (the default 24h overlap)", gotOverlapExpiresAt, wantOverlapExpiresAt)
		}
	})
	newAuth := "Bearer " + rotated.Key

	t.Run("the new secret authenticates immediately after rotation (AC4)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", newAuth, `{"name":"Post-Rotation User"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("the new secret appears nowhere except the rotate response itself (AC1)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		if strings.Contains(w.Body.String(), rotated.Key) {
			t.Fatalf("list response %s contains the raw rotated secret %q", w.Body.String(), rotated.Key)
		}

		rows, err := wired.DB.QueryContext(context.Background(), "SELECT lookup_prefix, secret_hash FROM server_api_key_secrets WHERE key_id = ?", issued.ID)
		if err != nil {
			t.Fatalf("dump server_api_key_secrets: %v", err)
		}
		defer rows.Close()
		rowCount := 0
		for rows.Next() {
			rowCount++
			var lookupPrefix, secretHash string
			if err := rows.Scan(&lookupPrefix, &secretHash); err != nil {
				t.Fatalf("scan row: %v", err)
			}
			remainder := strings.TrimPrefix(rotated.Key, rotated.Prefix)
			if strings.Contains(secretHash, remainder) || strings.Contains(lookupPrefix, remainder) {
				t.Errorf("database row (prefix=%q hash=%q) contains the rotated secret's remainder", lookupPrefix, secretHash)
			}
		}
		if rowCount != 2 {
			t.Fatalf("dumped %d server_api_key_secrets rows for the key, want exactly 2 (the outgoing one still inside its overlap window, plus the fresh one)", rowCount)
		}
	})

	t.Run("the old secret still authenticates inside the default 24h overlap window (AC2)", func(t *testing.T) {
		clock.Advance(23*time.Hour + 59*time.Minute)
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Still-Inside-Window User"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("listing the key shows rotatedAt and overlapExpiresAt, and never a secret field (AC5)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var entries []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode list: %v; body=%s", err, w.Body.String())
		}
		if len(entries) != 1 {
			t.Fatalf("len(entries) = %d, want 1", len(entries))
		}
		for _, unwantedField := range []string{"key", "secret", "secretHash"} {
			if _, present := entries[0][unwantedField]; present {
				t.Errorf("list entry %+v carries a %q field — a secret must never appear in List", entries[0], unwantedField)
			}
		}
		if entries[0]["rotatedAt"] == nil || entries[0]["rotatedAt"] == "" {
			t.Errorf("list entry %+v has no rotatedAt after a rotation", entries[0])
		}
		if entries[0]["overlapExpiresAt"] == nil || entries[0]["overlapExpiresAt"] == "" {
			t.Errorf("list entry %+v has no overlapExpiresAt after a rotation", entries[0])
		}
		if entries[0]["prefix"] != rotated.Prefix {
			t.Errorf("list entry prefix = %v, want the currently active (rotated) secret's prefix %q", entries[0]["prefix"], rotated.Prefix)
		}
	})

	t.Run("after the overlap window ends, the old secret is rejected as unauthorized (AC3)", func(t *testing.T) {
		clock.Advance(2 * time.Minute) // now well past the 24h mark
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Should-Be-Rejected User"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "unauthorized" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "unauthorized")
		}
	})

	t.Run("the new secret is unaffected by the old secret's expiry", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", newAuth, `{"name":"Still-Fine User"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("revoking the key immediately rejects both the (already-expired) old secret and the new secret (AC6)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/organizations/"+org.ID+"/api-keys/"+issued.ID, adminAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("revoke status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}

		oldAfterRevoke := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Old-After-Revoke"}`)
		if oldAfterRevoke.Code != http.StatusUnauthorized {
			t.Errorf("old secret status after revoke = %d, want %d; body=%s", oldAfterRevoke.Code, http.StatusUnauthorized, oldAfterRevoke.Body.String())
		}
		newAfterRevoke := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", newAuth, `{"name":"New-After-Revoke"}`)
		if newAfterRevoke.Code != http.StatusUnauthorized {
			t.Errorf("new secret status after revoke = %d, want %d; body=%s", newAfterRevoke.Code, http.StatusUnauthorized, newAfterRevoke.Body.String())
		}
	})
}

// TestKeyRotationJourney_CustomOverlapHoursIsHonored is AC2's "settable per
// rotation" half: an admin-chosen overlapHours (rather than the 24h default)
// governs exactly when the old secret stops authenticating.
func TestKeyRotationJourney_CustomOverlapHoursIsHonored(t *testing.T) {
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, nil, clock.Now)
	adminAuth := "Bearer " + support.AdminAPIKey
	org, issued := createOrgAndKey(t, wired.Router, adminAuth, "Custom Overlap Co")
	originalAuth := "Bearer " + issued.Key

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys/"+issued.ID+"/rotate", adminAuth, `{"overlapHours":2}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("rotate status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var rotated rotatedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	wantOverlapExpiresAt := clock.Now().Add(2 * time.Hour)
	gotOverlapExpiresAt, err := time.Parse(rfc3339MillisLayout, rotated.OverlapExpiresAt)
	if err != nil {
		t.Fatalf("parse overlapExpiresAt %q: %v", rotated.OverlapExpiresAt, err)
	}
	if !gotOverlapExpiresAt.Equal(wantOverlapExpiresAt) {
		t.Fatalf("overlapExpiresAt = %v, want %v (the requested 2h overlap, not the 24h default)", gotOverlapExpiresAt, wantOverlapExpiresAt)
	}

	t.Run("the old secret still authenticates just inside the custom 2h window", func(t *testing.T) {
		clock.Advance(1*time.Hour + 59*time.Minute)
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Inside Custom Window"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	t.Run("the old secret is rejected once the custom 2h window ends", func(t *testing.T) {
		clock.Advance(1 * time.Minute) // now past the 2h mark
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", originalAuth, `{"name":"Past Custom Window"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})
}

const rfc3339MillisLayout = "2006-01-02T15:04:05.000Z07:00"
