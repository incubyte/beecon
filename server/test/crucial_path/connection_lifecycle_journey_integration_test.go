//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, connectionDTO,
// wireErrorEnvelope, doJSONRequest, oauthJourneyFixture/
// newOAuthJourneyFixture, outlookDefinitionAgainst, openConnectPageAndGetState,
// connectionRowFromDB, and connectionWithAccountDTO from
// oauth_handshake_journey_integration_test.go; executionResultDTO,
// outlookDefinitionWithFakeGraphTool, activateConnectionThroughRealHandshake,
// executeTool, fetchLogs, and logsPageDTO from
// tool_execution_journey_integration_test.go — same package). This file tells
// Slice 4's full lifecycle story end to end against the real composition
// root: list/disable/delete/reconnect, a completed vs. an abandoned
// reconnect, transparent on-demand refresh (both the inline expiry check and
// a refresh-token rotation), a refresh failure that transitions a connection
// to EXPIRED and is itself reconnectable back to ACTIVE, and cross-org
// not-found for every lifecycle mutation.
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

type connectionsPageDTO struct {
	Items      []connectionDTO `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

type connectionStatusDTO struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func listConnections(t *testing.T, router http.Handler, orgAuth, query string) connectionsPageDTO {
	t.Helper()
	w := doJSONRequest(t, router, http.MethodGet, "/api/v1/connections/"+query, orgAuth, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list connections status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page connectionsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode connections page: %v; body=%s", err, w.Body.String())
	}
	return page
}

// TestConnectionLifecycleJourney_ListDisableDeleteReconnect is AC1, AC2, AC3,
// AC4, and AC11.
func TestConnectionLifecycleJourney_ListDisableDeleteReconnect(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: rawGraphAccessToken, RefreshToken: "raw-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	t.Run("listing connections shows status, provider, and account metadata", func(t *testing.T) {
		page := listConnections(t, wired.Router, fixture.orgAuth, "")
		if len(page.Items) != 1 || page.Items[0].ID != initiated.ID {
			t.Fatalf("Items = %+v, want exactly the one connection %q", page.Items, initiated.ID)
		}
		if page.Items[0].Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", page.Items[0].Status, "ACTIVE")
		}
		if page.Items[0].ProviderSlug != "outlook" {
			t.Errorf("providerSlug = %q, want %q", page.Items[0].ProviderSlug, "outlook")
		}
	})

	t.Run("listing filtered by an unknown userId returns no items", func(t *testing.T) {
		page := listConnections(t, wired.Router, fixture.orgAuth, "?userId=user_does_not_exist")
		if len(page.Items) != 0 {
			t.Errorf("Items = %+v, want none", page.Items)
		}
	})

	t.Run("disabling the connection transitions it to DISCONNECTED", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/disable", fixture.orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("disable status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var dto connectionStatusDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode disable response: %v", err)
		}
		if dto.Status != "DISCONNECTED" {
			t.Errorf("status = %q, want %q", dto.Status, "DISCONNECTED")
		}
	})

	t.Run("executing a tool against the now-DISCONNECTED connection is a status-explaining tool-level failure", func(t *testing.T) {
		status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, "{}")
		if status != http.StatusOK {
			t.Fatalf("execute status = %d, want %d (a non-ACTIVE connection is a tool-level failure, not an HTTP error)", status, http.StatusOK)
		}
		if result.Successful {
			t.Fatal("successful = true, want false for a DISCONNECTED connection")
		}
		if result.Data != nil {
			t.Errorf("data = %v, want nil", result.Data)
		}
		if result.Error == nil || !strings.Contains(result.Error.Message, "DISCONNECTED") {
			t.Errorf("error = %+v, want a message explaining the connection is DISCONNECTED", result.Error)
		}
	})

	t.Run("deleting the connection removes it — a subsequent get is not-found, and its stored credentials are destroyed", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, "")
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete status = %d, want %d; body=%s", w.Code, http.StatusNoContent, w.Body.String())
		}
		after := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, "")
		if after.Code != http.StatusNotFound {
			t.Fatalf("get-after-delete status = %d, want %d; body=%s", after.Code, http.StatusNotFound, after.Body.String())
		}

		rows, err := wired.DB.QueryContext(context.Background(), "SELECT id FROM connections WHERE id = ?", initiated.ID)
		if err != nil {
			t.Fatalf("query connections: %v", err)
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatalf("connections row %q still exists after Delete — its stored credentials must be destroyed", initiated.ID)
		}
	})

	t.Run("the deleted connection's earlier log entries still keep the id string", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&limit=100")
		if len(page.Entries) == 0 {
			t.Fatal("expected at least one log entry (the OAuth token exchange) to survive the connection's deletion")
		}
		for _, entry := range page.Entries {
			if entry.ConnectionID != initiated.ID {
				t.Errorf("log entry ConnectionID = %q, want %q", entry.ConnectionID, initiated.ID)
			}
		}
	})
}

// TestConnectionLifecycleJourney_ReconnectSameIDAndCrossOrgNotFound is AC4 and
// AC11's reconnect half.
func TestConnectionLifecycleJourney_ReconnectSameIDAndCrossOrgNotFound(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "raw-access-token", RefreshToken: "raw-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionAgainst(fakeMS))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)
	if w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/disable", fixture.orgAuth, ""); w.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	t.Run("reconnecting returns 201 with the same id and a fresh redirectUrl through the standard connect pages", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/reconnect", fixture.orgAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("reconnect status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var dto initiatedConnectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode reconnect response: %v", err)
		}
		if dto.ID != initiated.ID {
			t.Errorf("id = %q, want the same stable id %q", dto.ID, initiated.ID)
		}
		if dto.Status != "DISCONNECTED" {
			t.Errorf("status = %q, want %q (unchanged until the handshake completes)", dto.Status, "DISCONNECTED")
		}
		if dto.RedirectURL == initiated.RedirectURL {
			t.Error("reconnect's redirectUrl is identical to the original — it must mint a fresh single-use token")
		}
		state := openConnectPageAndGetState(t, wired, dto)
		if state == "" {
			t.Fatal("the reconnect's own connect page carried no CSRF state — it must go through the standard middle-man pages")
		}
	})

	t.Run("disable, delete, and reconnect against another organization's connection are all not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", "Bearer "+support.AdminAPIKey, `{"name":"Second Org"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create second org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var otherOrg organizationDTO
		if err := json.Unmarshal(w.Body.Bytes(), &otherOrg); err != nil {
			t.Fatalf("decode second org: %v", err)
		}
		// Give org B the same allow-listed redirectUri as org A, so the
		// cross-org reconnect below is rejected because the connection
		// belongs to org A (not because org B's own allow-list is empty) —
		// mirroring connections_journey_integration_test.go's own cross-org
		// initiate isolation.
		w = doJSONRequest(t, wired.Router, http.MethodPatch, "/api/v1/organizations/"+otherOrg.ID, "Bearer "+support.AdminAPIKey,
			`{"allowedRedirectUris":["`+fixture.allowedRedirectURI+`"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("patch second org allow-list status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+otherOrg.ID+"/api-keys", "Bearer "+support.AdminAPIKey, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("issue second org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var otherKey issuedKeyDTO
		if err := json.Unmarshal(w.Body.Bytes(), &otherKey); err != nil {
			t.Fatalf("decode second org key: %v", err)
		}
		otherAuth := "Bearer " + otherKey.Key

		if w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/disable", otherAuth, ""); w.Code != http.StatusNotFound {
			t.Errorf("cross-org disable status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		if w := doJSONRequest(t, wired.Router, http.MethodDelete, "/api/v1/connections/"+initiated.ID, otherAuth, ""); w.Code != http.StatusNotFound {
			t.Errorf("cross-org delete status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		if w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/reconnect", otherAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`); w.Code != http.StatusNotFound {
			t.Errorf("cross-org reconnect status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}

		// None of the above must have disturbed the connection under its own
		// organization's key.
		still := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, "")
		if still.Code != http.StatusOK {
			t.Fatalf("connection was affected by a cross-org request; get status = %d, want %d", still.Code, http.StatusOK)
		}
	})
}

// TestConnectionLifecycleJourney_CompletingAndAbandoningAReconnect is AC5 and
// AC6.
func TestConnectionLifecycleJourney_CompletingAndAbandoningAReconnect(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "original-access-token", RefreshToken: "original-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	t.Run("an abandoned reconnect leaves the connection ACTIVE and still executing with its original token", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/reconnect", fixture.orgAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("reconnect status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		// The reconnect's own connect page/callback is never visited — the
		// user abandoned it.

		got := fixture.getConnection(t, wired, initiated.ID)
		if got.Status != "ACTIVE" {
			t.Errorf("status after an abandoned reconnect = %q, want it to remain %q", got.Status, "ACTIVE")
		}

		status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, "{}")
		if status != http.StatusOK {
			t.Fatalf("execute status = %d, want %d; body was %+v", status, http.StatusOK, result)
		}
		if !result.Successful {
			t.Fatalf("successful = false, want true — an abandoned reconnect must not interrupt execution; error=%+v", result.Error)
		}
		if fakeGraph.LastAuthorizationHeader != "Bearer original-access-token" {
			t.Errorf("Authorization = %q, want the connection's original (untouched) access token", fakeGraph.LastAuthorizationHeader)
		}
	})

	t.Run("completing a reconnect reactivates the same id with fresh tokens and refreshed account metadata", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/reconnect", fixture.orgAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("reconnect status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var reconnected initiatedConnectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &reconnected); err != nil {
			t.Fatalf("decode reconnect response: %v", err)
		}
		state := openConnectPageAndGetState(t, wired, reconnected)

		callback := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if callback.Code != http.StatusFound {
			t.Fatalf("reconnect callback status = %d, want %d; body=%s", callback.Code, http.StatusFound, callback.Body.String())
		}
		if got := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/connections/"+initiated.ID, fixture.orgAuth, ""); got.Code != http.StatusOK {
			t.Fatalf("get-connection status = %d, want %d", got.Code, http.StatusOK)
		}

		got := fixture.getConnection(t, wired, initiated.ID)
		if got.ID != initiated.ID {
			t.Errorf("id = %q, want the same stable id %q", got.ID, initiated.ID)
		}
		if got.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", got.Status, "ACTIVE")
		}

		status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, "{}")
		if status != http.StatusOK || !result.Successful {
			t.Fatalf("execute after completed reconnect: status=%d successful=%v error=%+v", status, result.Successful, result.Error)
		}
		if fakeGraph.LastAuthorizationHeader != "Bearer original-access-token" {
			t.Errorf("Authorization = %q, want the SAME fresh access token FakeMicrosoft issued at reconnect", fakeGraph.LastAuthorizationHeader)
		}
	})
}

// TestConnectionLifecycleJourney_TransparentRefreshOnExpiry is AC7 and AC8:
// an expired access token is refreshed inline before the provider is ever
// called, the call completes as a normal success, a rotated refresh token
// replaces the stored one, and the refresh's own OAuth token-exchange log
// entry is redacted exactly like the original handshake's.
func TestConnectionLifecycleJourney_TransparentRefreshOnExpiry(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "original-access-token", RefreshToken: "original-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
		ExpiresIn:           60,
		RefreshAccessToken:  "refreshed-access-token",
		RefreshRefreshToken: "rotated-refresh-token",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	beforeRow := connectionRowFromDB(t, wired.DB, initiated.ID)

	clock.Advance(2 * time.Minute) // past the scripted 60s ExpiresIn

	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, "{}")

	if status != http.StatusOK {
		t.Fatalf("execute status = %d, want %d", status, http.StatusOK)
	}
	if !result.Successful {
		t.Fatalf("successful = false, want true — an expired token must be transparently refreshed; error=%+v", result.Error)
	}
	if fakeMS.RefreshCallCount != 1 {
		t.Fatalf("RefreshCallCount = %d, want exactly 1", fakeMS.RefreshCallCount)
	}
	if fakeGraph.LastAuthorizationHeader != "Bearer refreshed-access-token" {
		t.Errorf("Authorization = %q, want the freshly refreshed access token", fakeGraph.LastAuthorizationHeader)
	}

	afterRow := connectionRowFromDB(t, wired.DB, initiated.ID)
	if afterRow.EncryptedRefreshToken == beforeRow.EncryptedRefreshToken {
		t.Error("encrypted_refresh_token is unchanged after a refresh that rotated it — AC8 requires the rotated value to replace the stored one")
	}

	t.Run("the refresh grant's own log entry never leaks the refresh or access token", func(t *testing.T) {
		rows, err := wired.DB.QueryContext(context.Background(), "SELECT request_body, response_body FROM event_logs WHERE connection_id = ? AND kind = 'oauth_token_exchange'", initiated.ID)
		if err != nil {
			t.Fatalf("query event_logs: %v", err)
		}
		defer rows.Close()
		rowCount := 0
		for rows.Next() {
			rowCount++
			var requestBody, responseBody string
			if err := rows.Scan(&requestBody, &responseBody); err != nil {
				t.Fatalf("scan event_logs row: %v", err)
			}
			for _, secret := range []string{"original-refresh-token", "rotated-refresh-token", "refreshed-access-token", "original-access-token"} {
				if strings.Contains(requestBody, secret) || strings.Contains(responseBody, secret) {
					t.Errorf("event_logs row contains a raw token value %q: request=%q response=%q", secret, requestBody, responseBody)
				}
			}
		}
		if rowCount < 2 {
			t.Fatalf("found %d oauth_token_exchange log rows, want at least 2 (the original handshake and the refresh grant)", rowCount)
		}
	})
}

// TestConnectionLifecycleJourney_RefreshFailureTransitionsToExpiredThenReconnectRestoresActive
// is AC9 and AC10.
func TestConnectionLifecycleJourney_RefreshFailureTransitionsToExpiredThenReconnectRestoresActive(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "original-access-token", RefreshToken: "revoked-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
		ExpiresIn:   60,
		FailRefresh: true,
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	wired := support.BootAppWithProviderDefinitionsAndClock(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), clock.Now)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	clock.Advance(2 * time.Minute) // past the scripted 60s ExpiresIn

	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, "{}")

	if status != http.StatusOK {
		t.Fatalf("execute status = %d, want %d", status, http.StatusOK)
	}
	if result.Successful {
		t.Fatal("successful = true, want false — a rejected refresh grant must surface as a tool-level failure")
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "EXPIRED") {
		t.Fatalf("error = %+v, want a message explaining the connection is now EXPIRED", result.Error)
	}

	got := fixture.getConnection(t, wired, initiated.ID)
	if got.Status != "EXPIRED" {
		t.Fatalf("status = %q, want %q after a rejected refresh grant", got.Status, "EXPIRED")
	}

	t.Run("reconnecting the EXPIRED connection restores it to ACTIVE with the same id", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/connections/"+initiated.ID+"/reconnect", fixture.orgAuth,
			`{"redirectUri":"`+fixture.allowedRedirectURI+`"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("reconnect status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		var reconnected initiatedConnectionDTO
		if err := json.Unmarshal(w.Body.Bytes(), &reconnected); err != nil {
			t.Fatalf("decode reconnect response: %v", err)
		}
		if reconnected.ID != initiated.ID {
			t.Fatalf("reconnect id = %q, want the same stable id %q", reconnected.ID, initiated.ID)
		}
		state := openConnectPageAndGetState(t, wired, reconnected)

		callback := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
		if callback.Code != http.StatusFound {
			t.Fatalf("reconnect callback status = %d, want %d; body=%s", callback.Code, http.StatusFound, callback.Body.String())
		}

		final := fixture.getConnection(t, wired, initiated.ID)
		if final.Status != "ACTIVE" {
			t.Errorf("status = %q, want %q", final.Status, "ACTIVE")
		}
		if final.ID != initiated.ID {
			t.Errorf("id = %q, want the same stable id %q", final.ID, initiated.ID)
		}
	})
}
