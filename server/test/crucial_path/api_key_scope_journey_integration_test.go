//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, wireErrorEnvelope, doJSONRequest,
// userDTO, integrationSummaryDTO, initiatedConnectionDTO, connectionDTO,
// connectionStatusDTO from files already declared there; browserTokenFixture
// and newBrowserTokenFixture from browser_token_journey_integration_test.go —
// same package). This file tells Slice 4's scope-enforcement story end to
// end against the real composition root (PD41): an org issued both a
// read-only and a read-write key sees the read-only key rejected with a
// scope-explaining 403 on every mutating route — including
// webhook-endpoint/test, which the verifier ruled mutating (it persists an
// evt_ outbox event and triggers an outbound delivery attempt), so it is
// requireWrite-guarded like PUT webhook-endpoint rather than left open like
// GET/list/inspect — while every read/list/inspect call and the
// deliberately-unwrapped connections/initiate route keep working; a key
// issued with no scope specified at all (standing in for every key issued
// before this phase, since ParseScope("") defaults to read-write) keeps full
// access, including webhook-endpoint/test; and a user-token request on the
// orgOrUser reconnect route is never blocked by RequireWrite, because scope
// is an org-key concept only.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"

	"beecon/internal/app"
	"beecon/test/support"
)

// scopedIssuedKeyDTO is issuedKeyDTO (access_users_journey_integration_test.go)
// plus the scope field (PD41) — a separate type, rather than adding the field
// to that shared struct, so this file's own subject (scope) stays visible in
// its own type name without touching a fixture every other journey in this
// package also uses.
type scopedIssuedKeyDTO struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Prefix    string `json:"prefix"`
	Scope     string `json:"scope"`
	CreatedAt string `json:"createdAt"`
}

// issueScopedKey issues an org API key with the given scope body (empty
// scopeBody reproduces "no scope specified at all" — a pre-Slice-4 caller's
// exact request shape, defaulting to read-write via access.ParseScope).
func issueScopedKey(t *testing.T, wired *app.Wired, adminAuth, orgID, scopeBody string) scopedIssuedKeyDTO {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+orgID+"/api-keys", adminAuth, scopeBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var key scopedIssuedKeyDTO
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode issued key: %v", err)
	}
	return key
}

// scopeJourneyFixture is the org/integration/redirect-uri scaffolding every
// sub-test in this file needs before it can issue its own scoped keys.
type scopeJourneyFixture struct {
	orgID         string
	integrationID string
	redirectURI   string
}

func newScopeJourneyFixture(t *testing.T, wired *app.Wired, adminAuth string) scopeJourneyFixture {
	t.Helper()
	const redirectURI = "https://consumer.example.com/callback"

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Scope Journey Org"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var org organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+org.ID, adminAuth,
		`{"allowedRedirectUris":["`+redirectURI+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var integration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"client-id","clientSecret":"client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &integration); err != nil {
		t.Fatalf("decode integration: %v", err)
	}

	return scopeJourneyFixture{orgID: org.ID, integrationID: integration.ID, redirectURI: redirectURI}
}

func initiateConnection(t *testing.T, wired *app.Wired, orgAuth, userID, integrationID, redirectURI string) (int, initiatedConnectionDTO) {
	t.Helper()
	body := `{"userId":"` + userID + `","integrationId":"` + integrationID + `","redirectUri":"` + redirectURI + `"}`
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", orgAuth, body)
	var conn initiatedConnectionDTO
	if w.Code == http.StatusCreated {
		if err := json.Unmarshal(w.Body.Bytes(), &conn); err != nil {
			t.Fatalf("decode initiated connection: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, conn
}

// TestApiKeyScopeJourney_AReadOnlyKeyIsRejectedOnMutatingRoutesAndAllowedOnReadRoutesWhileAReadWriteKeyDoesBoth
// is Slice 4's central security guarantee (PD41), proven end to end: issuing
// a read-only and a read-write key for the same org, the read-only key is
// rejected with a scope-explaining 403 on representative mutating routes
// (user creation, connection disable) while every GET/list call and the
// deliberately-unwrapped initiate route keep succeeding; the read-write key
// succeeds at everything.
func TestApiKeyScopeJourney_AReadOnlyKeyIsRejectedOnMutatingRoutesAndAllowedOnReadRoutesWhileAReadWriteKeyDoesBoth(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	fixture := newScopeJourneyFixture(t, wired, adminAuth)

	readOnlyKey := issueScopedKey(t, wired, adminAuth, fixture.orgID, `{"scope":"read-only"}`)
	readWriteKey := issueScopedKey(t, wired, adminAuth, fixture.orgID, `{"scope":"read-write"}`)
	readOnlyAuth := "Bearer " + readOnlyKey.Key
	readWriteAuth := "Bearer " + readWriteKey.Key

	if readOnlyKey.Scope != "read-only" {
		t.Fatalf("issued key's own scope = %q, want %q", readOnlyKey.Scope, "read-only")
	}
	if readWriteKey.Scope != "read-write" {
		t.Fatalf("issued key's own scope = %q, want %q", readWriteKey.Scope, "read-write")
	}

	t.Run("a read-only key is rejected with a scope-explaining 403 creating a user", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", readOnlyAuth, `{"name":"Should Be Rejected"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "forbidden" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "forbidden")
		}
		if env.Error.Message == "" {
			t.Error("error.message is empty, want a scope-explaining message")
		}
	})

	t.Run("a read-write key succeeds creating a user", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", readWriteAuth, `{"name":"Ada Lovelace"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
	})

	// A user to drive connections/initiate below — created with the
	// read-write key so this setup step isn't itself part of what's under
	// test.
	var user userDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", readWriteAuth, `{"name":"Connection Owner"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	t.Run("connections/initiate is deliberately not requireWrite-guarded — a read-only key can still initiate", func(t *testing.T) {
		status, _ := initiateConnection(t, wired, readOnlyAuth, user.ID, fixture.integrationID, fixture.redirectURI)
		if status != http.StatusCreated {
			t.Fatalf("status = %d, want %d — initiate is a read-only-allowed route", status, http.StatusCreated)
		}
	})

	t.Run("GET /connections (list) succeeds for a read-only key", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections", readOnlyAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	readOnlyStatus, readOnlyConn := initiateConnection(t, wired, readOnlyAuth, user.ID, fixture.integrationID, fixture.redirectURI)
	if readOnlyStatus != http.StatusCreated {
		t.Fatalf("initiate (for the disable sub-test) status = %d, want %d", readOnlyStatus, http.StatusCreated)
	}

	t.Run("a read-only key is rejected with 403 disabling a connection (a representative mutating route)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+readOnlyConn.ID+"/disable", readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	readWriteStatus, readWriteConn := initiateConnection(t, wired, readWriteAuth, user.ID, fixture.integrationID, fixture.redirectURI)
	if readWriteStatus != http.StatusCreated {
		t.Fatalf("initiate (for the read-write disable sub-test) status = %d, want %d", readWriteStatus, http.StatusCreated)
	}

	t.Run("a read-write key succeeds disabling its own connection", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+readWriteConn.ID+"/disable", readWriteAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var status connectionStatusDTO
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode disable response: %v", err)
		}
		if status.Status != "DISCONNECTED" {
			t.Errorf("status = %q, want %q", status.Status, "DISCONNECTED")
		}
	})

	// Setup shared by both webhook-endpoint sub-tests below: a read-write
	// key sets the org's endpoint once, since PUT itself is
	// requireWrite-guarded (proven separately below).
	setEndpointResp := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/webhook-endpoint/", readWriteAuth, `{"url":"https://example.com/hook"}`)
	if setEndpointResp.Code != http.StatusOK {
		t.Fatalf("set endpoint (read-write) status = %d, want %d; body=%s", setEndpointResp.Code, http.StatusOK, setEndpointResp.Body.String())
	}

	// webhook-endpoint/test IS requireWrite-guarded (verifier ruling,
	// PD41): sending a test webhook persists an evt_ outbox event and
	// triggers an outbound delivery attempt — a real mutation, not a
	// read-only inspection — so it belongs in the "rejected for read-only"
	// group alongside user-creation/connection-disable/PUT-endpoint, not
	// the "allowed" group with GET/list/inspect and initiate.
	t.Run("a read-only key is rejected with 403 sending a test webhook (webhook-endpoint/test mutates: it persists an event and triggers delivery)", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoint/test", readOnlyAuth, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("a read-write key succeeds sending a test webhook", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/webhook-endpoint/test", readWriteAuth, "")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	})

	t.Run("PUT webhook-endpoint IS requireWrite-guarded — a read-only key is rejected with 403", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPut, "/api/v1/webhook-endpoint/", readOnlyAuth, `{"url":"https://example.com/should-be-rejected"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusForbidden, w.Body.String())
		}
	})
}

// TestApiKeyScopeJourney_AKeyIssuedWithNoScopeSpecifiedAtAllRetainsFullAccess
// stands in for every key issued before this phase existed (PD41's own AC):
// issuing a key through the exact request shape a pre-Slice-4 caller would
// have sent (no "scope" field at all) must default to read-write and so
// retain full access to a mutating route, not merely succeed at reads.
func TestApiKeyScopeJourney_AKeyIssuedWithNoScopeSpecifiedAtAllRetainsFullAccess(t *testing.T) {
	wired := support.BootApp(t)
	adminAuth := "Bearer " + support.AdminAPIKey
	fixture := newScopeJourneyFixture(t, wired, adminAuth)

	preExistingStyleKey := issueScopedKey(t, wired, adminAuth, fixture.orgID, "")
	if preExistingStyleKey.Scope != "read-write" {
		t.Fatalf("a key issued with no scope field at all got scope %q, want %q (the PD41 backward-compatibility default)", preExistingStyleKey.Scope, "read-write")
	}
	preExistingStyleAuth := "Bearer " + preExistingStyleKey.Key

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", preExistingStyleAuth, `{"name":"Legacy Caller"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s (a pre-Slice-4-style key must retain full mutating access)", w.Code, http.StatusCreated, w.Body.String())
	}
	var user userDTO
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	status, conn := initiateConnection(t, wired, preExistingStyleAuth, user.ID, fixture.integrationID, fixture.redirectURI)
	if status != http.StatusCreated {
		t.Fatalf("initiate status = %d, want %d", status, http.StatusCreated)
	}

	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+conn.ID+"/disable", preExistingStyleAuth, "")
	if w.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d; body=%s (a pre-Slice-4-style key must retain full mutating access)", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestApiKeyScopeJourney_AUserTokenRequestOnTheReconnectRouteIsNeverBlockedByRequireWrite
// pins RequireWrite's own no-op case (authmw/admin.go's doc comment): scope
// is an org-key concept only, so a user-token request reaching an
// orgOrUser+RequireWrite-guarded route (reconnect) must never be rejected for
// lack of write scope — there is no scope to check in the first place.
func TestApiKeyScopeJourney_AUserTokenRequestOnTheReconnectRouteIsNeverBlockedByRequireWrite(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newBrowserTokenFixture(t, wired)

	userID := fixture.createUser(t, wired, "Ada Lovelace")
	token := fixture.mintValidTokenFor(t, userID)

	body := `{"integrationId":"` + fixture.integrationID + `","redirectUri":"` + fixture.allowedRedirectURI + `"}`
	initiateResp := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/initiate", "Bearer "+token, body)
	if initiateResp.Code != http.StatusCreated {
		t.Fatalf("initiate status = %d, want %d; body=%s", initiateResp.Code, http.StatusCreated, initiateResp.Body.String())
	}
	var conn initiatedConnectionDTO
	if err := json.Unmarshal(initiateResp.Body.Bytes(), &conn); err != nil {
		t.Fatalf("decode initiated connection: %v", err)
	}

	// Reconnect is only allowed from ACTIVE/EXPIRED/DISCONNECTED (Phase 3
	// Slice 4) — the freshly initiated connection is still INITIATED, so
	// bring it to DISCONNECTED via the org API key first (the same shortcut
	// TestBrowserTokenJourney_ValidTokenDrivesTheBrowserSurface's own
	// reconnect sub-test uses); the reconnect call itself below is still
	// driven entirely by the user token, which is the thing under test.
	disable := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+conn.ID+"/disable", fixture.orgAuth, "")
	if disable.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d; body=%s", disable.Code, http.StatusOK, disable.Body.String())
	}

	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+conn.ID+"/reconnect", "Bearer "+token,
		`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("reconnect status = %d, want %d; body=%s — a user token carries no scope, so RequireWrite must never block it", w.Code, http.StatusCreated, w.Body.String())
	}
}
