//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope,
// doJSONRequest, oauthJourneyFixture/newOAuthJourneyFixture, and
// openConnectPageAndGetState from oauth_handshake_journey_integration_test.go
// — same package). This file tells Slice 5's story end to end against the
// real composition root and a FakeGraph httptest server standing in for
// Microsoft Graph: a live ACTIVE connection (built through the real OAuth
// handshake against FakeMicrosoft) executes outlook-list-messages, arguments
// are validated against the tool's schema before Graph is ever called,
// unknown tools/cross-org/cross-user connections are rejected before any
// provider call, upstream Graph errors surface as tool-level failures, every
// execution and token exchange writes a redacted log entry, and the logs API
// filters/paginates/isolates by organization.
package crucial_path

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

type executionErrorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type executionResultDTO struct {
	Successful bool               `json:"successful"`
	Error      *executionErrorDTO `json:"error"`
	Data       any                `json:"data"`
}

type logEntryDTO struct {
	ID           string `json:"id"`
	OrgID        string `json:"organizationId"`
	UserID       string `json:"userId"`
	ConnectionID string `json:"connectionId"`
	ToolSlug     string `json:"toolSlug"`
	Kind         string `json:"kind"`
	Status       int    `json:"status"`
	DurationMs   int64  `json:"durationMs"`
	RequestBody  string `json:"requestBody"`
	ResponseBody string `json:"responseBody"`
	RateLimited  bool   `json:"rateLimited"`
	CreatedAt    string `json:"createdAt"`
}

type logsPageDTO struct {
	Entries    []logEntryDTO `json:"entries"`
	NextCursor string        `json:"nextCursor"`
}

const rawGraphAccessToken = "raw-microsoft-access-token-for-tool-execution"

// outlookDefinitionWithFakeGraphTool is like outlookDefinitionAgainst
// (oauth_handshake_journey_integration_test.go) but also declares the
// outlook-list-messages tool, pointed at fakeGraph instead of the real
// internet (PD8).
func outlookDefinitionWithFakeGraphTool(fakeMS *support.FakeMicrosoft, fakeGraph *support.FakeGraph) []catalog.ProviderDefinition {
	definitions := outlookDefinitionAgainst(fakeMS)
	definitions[0].Tools = []catalog.ProviderTool{
		{
			Slug:   "outlook-list-messages",
			Name:   "List messages",
			Method: "GET",
			Path:   fakeGraph.MessagesURL,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"top":    map[string]any{"type": "integer"},
					"skip":   map[string]any{"type": "integer"},
					"select": map[string]any{"type": "string"},
					"filter": map[string]any{"type": "string"},
				},
			},
		},
	}
	return definitions
}

// activateConnectionThroughRealHandshake drives initiate -> connect page ->
// callback through live HTTP requests against the real composition root, so
// the resulting Connection is genuinely ACTIVE with a vault-encrypted token
// decryptable back to rawGraphAccessToken.
func activateConnectionThroughRealHandshake(t *testing.T, wired *app.Wired, fixture oauthJourneyFixture) initiatedConnectionDTO {
	t.Helper()
	initiated := fixture.initiate(t, wired)
	state := openConnectPageAndGetState(t, wired, initiated)
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/connect/oauth/callback?code=the-auth-code&state="+state, "", "")
	if w.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (handshake must complete); body=%s", w.Code, http.StatusFound, w.Body.String())
	}
	return initiated
}

func executeTool(t *testing.T, wired *app.Wired, orgAuth, slug, userID, connectionID, argumentsJSON string) (int, executionResultDTO) {
	t.Helper()
	body := fmt.Sprintf(`{"userId":%q,"connectionId":%q,"arguments":%s}`, userID, connectionID, argumentsJSON)
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/tools/"+slug+"/execute", orgAuth, body)
	var dto executionResultDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode execution result: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func fetchLogs(t *testing.T, wired *app.Wired, orgAuth, query string) logsPageDTO {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/logs/"+query, orgAuth, "")
	if w.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var page logsPageDTO
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode logs page: %v; body=%s", err, w.Body.String())
	}
	return page
}

func TestToolExecutionJourney_HappyPath(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: rawGraphAccessToken, RefreshToken: "raw-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	var result executionResultDTO
	t.Run("executing outlook-list-messages returns successful:true with the mailbox messages", func(t *testing.T) {
		status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID,
			`{"top":10,"skip":0,"select":"subject","filter":"isRead eq false"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		result = dto
		if !dto.Successful {
			t.Fatalf("successful = false, want true; error = %+v", dto.Error)
		}
		if dto.Error != nil {
			t.Errorf("error = %+v, want nil", dto.Error)
		}
		data, ok := dto.Data.(map[string]any)
		if !ok {
			t.Fatalf("data = %T, want a decoded JSON object with the mailbox messages", dto.Data)
		}
		if _, present := data["value"]; !present {
			t.Errorf("data %+v does not carry mailbox messages under \"value\"", data)
		}
	})

	t.Run("top/skip/select/filter arguments are forwarded to Graph as query parameters", func(t *testing.T) {
		query := url.Values(fakeGraph.LastQuery)
		if got := query.Get("top"); got != "10" {
			t.Errorf("top = %q, want %q", got, "10")
		}
		if got := query.Get("skip"); got != "0" {
			t.Errorf("skip = %q, want %q", got, "0")
		}
		if got := query.Get("select"); got != "subject" {
			t.Errorf("select = %q, want %q", got, "subject")
		}
		if got := query.Get("filter"); got != "isRead eq false" {
			t.Errorf("filter = %q, want %q", got, "isRead eq false")
		}
	})

	t.Run("Graph receives the connection's decrypted access token as the bearer token", func(t *testing.T) {
		if fakeGraph.LastAuthorizationHeader != "Bearer "+rawGraphAccessToken {
			t.Errorf("Authorization = %q, want %q", fakeGraph.LastAuthorizationHeader, "Bearer "+rawGraphAccessToken)
		}
	})

	t.Run("the raw access token never appears anywhere in the execution response", func(t *testing.T) {
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), rawGraphAccessToken) {
			t.Fatalf("execution response %s contains the raw access token", raw)
		}
	})

	var toolExecutionEntry, oauthEntry *logEntryDTO
	t.Run("the tool execution and the earlier OAuth token exchange each wrote a log entry", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID)
		for i := range page.Entries {
			entry := &page.Entries[i]
			switch entry.Kind {
			case "tool_execution":
				toolExecutionEntry = entry
			case "oauth_token_exchange":
				oauthEntry = entry
			}
		}
		if toolExecutionEntry == nil {
			t.Fatalf("no tool_execution log entry found among %+v", page.Entries)
		}
		if oauthEntry == nil {
			t.Fatalf("no oauth_token_exchange log entry found among %+v", page.Entries)
		}
		if toolExecutionEntry.OrgID == "" || toolExecutionEntry.UserID == "" || toolExecutionEntry.ConnectionID == "" {
			t.Errorf("tool_execution entry %+v is missing org/user/connection ids", toolExecutionEntry)
		}
		if toolExecutionEntry.ToolSlug != "outlook-list-messages" {
			t.Errorf("tool_execution entry toolSlug = %q, want %q", toolExecutionEntry.ToolSlug, "outlook-list-messages")
		}
		if toolExecutionEntry.Status != http.StatusOK {
			t.Errorf("tool_execution entry status = %d, want %d", toolExecutionEntry.Status, http.StatusOK)
		}
		if toolExecutionEntry.DurationMs < 0 {
			t.Errorf("tool_execution entry durationMs = %d, want >= 0", toolExecutionEntry.DurationMs)
		}
		if oauthEntry.ToolSlug != "" {
			t.Errorf("oauth_token_exchange entry toolSlug = %q, want empty", oauthEntry.ToolSlug)
		}
	})

	t.Run("redacted request/response bodies surface through the logs API without the raw secret", func(t *testing.T) {
		raw, _ := json.Marshal([]*logEntryDTO{toolExecutionEntry, oauthEntry})
		if strings.Contains(string(raw), rawGraphAccessToken) {
			t.Fatalf("logs API response contains the raw access token: %s", raw)
		}
		if !strings.Contains(oauthEntry.RequestBody, "[REDACTED]") && !strings.Contains(oauthEntry.ResponseBody, "[REDACTED]") {
			t.Errorf("oauth_token_exchange entry carries no redaction marker: request=%q response=%q", oauthEntry.RequestBody, oauthEntry.ResponseBody)
		}
		if !strings.Contains(toolExecutionEntry.RequestBody, "[REDACTED]") {
			t.Errorf("tool_execution entry's request body carries no redaction marker: %q", toolExecutionEntry.RequestBody)
		}
	})

	t.Run("the raw access token and client secret are absent from the raw event_logs database rows", func(t *testing.T) {
		rows, err := wired.DB.QueryContext(context.Background(),
			"SELECT request_body, response_body FROM event_logs WHERE connection_id = ?", initiated.ID)
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
			if strings.Contains(requestBody, rawGraphAccessToken) || strings.Contains(responseBody, rawGraphAccessToken) {
				t.Errorf("event_logs row contains the raw access token: request=%q response=%q", requestBody, responseBody)
			}
			if strings.Contains(requestBody, "the-client-secret") || strings.Contains(responseBody, "the-client-secret") {
				t.Errorf("event_logs row contains a raw client secret: request=%q response=%q", requestBody, responseBody)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate event_logs rows: %v", err)
		}
		if rowCount != 2 {
			t.Fatalf("dumped %d event_logs rows for this connection, want 2 (one oauth_token_exchange, one tool_execution)", rowCount)
		}
	})
}

func TestToolExecutionJourney_InvalidArgumentsNeverCallTheProvider(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{"top":"not-a-number"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (AC2: invalid arguments is a tool-level failure)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for invalid arguments")
	}
	if dto.Error == nil || dto.Error.Code != "invalid_arguments" {
		t.Errorf("error = %+v, want code %q", dto.Error, "invalid_arguments")
	}
	if dto.Data != nil {
		t.Errorf("data = %v, want nil", dto.Data)
	}
	if fakeGraph.LastQuery != nil {
		t.Fatalf("Graph received a request %v — invalid arguments must never reach the provider", fakeGraph.LastQuery)
	}
}

func TestToolExecutionJourney_UnknownToolSlugReturnsNotFound(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, _ := executeTool(t, wired, fixture.orgAuth, "unknown-tool-slug", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
}

func TestToolExecutionJourney_NonActiveConnectionReturnsFailureResultWithStatusExplainingError(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := fixture.initiate(t, wired) // never completes the handshake -> stays INITIATED

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (AC4: a non-ACTIVE connection is a tool-level failure)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for a non-ACTIVE connection")
	}
	if dto.Error == nil || dto.Error.Code != "connection_not_active" {
		t.Errorf("error = %+v, want code %q", dto.Error, "connection_not_active")
	}
	if dto.Error != nil && !strings.Contains(dto.Error.Message, "INITIATED") {
		t.Errorf("error.message = %q, want it to explain the connection is INITIATED", dto.Error.Message)
	}
	if fakeGraph.LastQuery != nil {
		t.Fatalf("Graph received a request %v — a non-ACTIVE connection must never reach the provider", fakeGraph.LastQuery)
	}
}

func TestToolExecutionJourney_CrossOrganizationConnectionReturnsNotFound(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	adminAuth := "Bearer " + support.AdminAPIKey
	var otherOrg organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Other Org"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create other org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &otherOrg); err != nil {
		t.Fatalf("decode other org: %v", err)
	}
	var otherKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+otherOrg.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue other org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &otherKey); err != nil {
		t.Fatalf("decode other key: %v", err)
	}

	status, _ := executeTool(t, wired, "Bearer "+otherKey.Key, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
	}
	if fakeGraph.LastQuery != nil {
		t.Fatalf("Graph received a request %v — a cross-org connection must never reach the provider", fakeGraph.LastQuery)
	}
}

func TestToolExecutionJourney_ConnectionNotBelongingToTheGivenUserIDReturnsErrorWithoutCallingTheProvider(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	var otherUser userDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", fixture.orgAuth, `{"name":"Grace Hopper"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &otherUser); err != nil {
		t.Fatalf("decode second user: %v", err)
	}

	status, _ := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", otherUser.ID, initiated.ID, `{}`)

	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (AC6: a connectionId not belonging to the given userId is an error)", status, http.StatusNotFound)
	}
	if fakeGraph.LastQuery != nil {
		t.Fatalf("Graph received a request %v — a connectionId/userId mismatch must never reach the provider", fakeGraph.LastQuery)
	}
}

func TestToolExecutionJourney_Upstream4xxFromGraphReturnsFailureResultSurfacingStatusAndMessage(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{StatusCode: http.StatusUnauthorized, Body: `{"error":"InvalidAuthenticationToken"}`})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (AC7: an upstream error is a tool-level failure)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for a 401 upstream response")
	}
	if dto.Error == nil || dto.Error.Code != "provider_error" {
		t.Fatalf("error = %+v, want code %q", dto.Error, "provider_error")
	}
	if !strings.Contains(dto.Error.Message, "401") {
		t.Errorf("error.message = %q, want it to surface the provider's status code", dto.Error.Message)
	}
	if !strings.Contains(dto.Error.Message, "InvalidAuthenticationToken") {
		t.Errorf("error.message = %q, want it to surface the provider's response body", dto.Error.Message)
	}
}

func TestToolExecutionJourney_Upstream5xxFromGraphReturnsFailureResultSurfacingStatusAndMessage(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{StatusCode: http.StatusServiceUnavailable, Body: "temporarily unavailable"})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for a 503 upstream response")
	}
	if dto.Error == nil || dto.Error.Code != "provider_error" {
		t.Fatalf("error = %+v, want code %q", dto.Error, "provider_error")
	}
	if !strings.Contains(dto.Error.Message, "503") {
		t.Errorf("error.message = %q, want it to surface the provider's status code", dto.Error.Message)
	}
}

// TestLogsAPIJourney_FiltersPaginationAndOrganizationIsolation covers PD10 /
// AC10: connectionId/userId/toolSlug/from/to each narrow the result set,
// cursor pagination walks newest-first without duplicates or gaps, and a
// second organization's key never sees the first organization's entries.
func TestLogsAPIJourney_FiltersPaginationAndOrganizationIsolation(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	const executionCount = 4
	for i := 0; i < executionCount; i++ {
		status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)
		if status != http.StatusOK || !dto.Successful {
			t.Fatalf("seed execution %d failed: status=%d dto=%+v", i, status, dto)
		}
	}

	t.Run("filtering by connectionId narrows to this connection's own entries", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&limit=100")
		// executionCount tool_execution entries + 1 oauth_token_exchange entry.
		if len(page.Entries) != executionCount+1 {
			t.Fatalf("got %d entries, want %d", len(page.Entries), executionCount+1)
		}
	})

	t.Run("filtering by toolSlug narrows to only tool_execution entries", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?toolSlug=outlook-list-messages&limit=100")
		if len(page.Entries) != executionCount {
			t.Fatalf("got %d entries, want %d", len(page.Entries), executionCount)
		}
		for _, entry := range page.Entries {
			if entry.ToolSlug != "outlook-list-messages" {
				t.Errorf("entry toolSlug = %q, want %q", entry.ToolSlug, "outlook-list-messages")
			}
		}
	})

	t.Run("filtering by userId narrows to this user's own entries", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?userId="+fixture.userID+"&limit=100")
		if len(page.Entries) != executionCount+1 {
			t.Fatalf("got %d entries, want %d", len(page.Entries), executionCount+1)
		}
	})

	t.Run("a future from/to range narrows the result set to nothing", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?from=2099-01-01T00:00:00Z&to=2099-12-31T00:00:00Z")
		if len(page.Entries) != 0 {
			t.Fatalf("got %d entries for a future time range, want 0", len(page.Entries))
		}
	})

	t.Run("cursor pagination walks newest-first without duplicates or gaps", func(t *testing.T) {
		seen := map[string]bool{}
		var order []string
		cursor := ""
		for page := 0; page < executionCount+2; page++ {
			result := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&limit=2&cursor="+cursor)
			for _, entry := range result.Entries {
				if seen[entry.ID] {
					t.Fatalf("entry id %q seen more than once while paginating", entry.ID)
				}
				seen[entry.ID] = true
				order = append(order, entry.CreatedAt)
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		if len(seen) != executionCount+1 {
			t.Fatalf("walked %d entries across all pages, want exactly %d (no duplicates or gaps)", len(seen), executionCount+1)
		}
		for i := 1; i < len(order); i++ {
			if order[i] > order[i-1] {
				t.Errorf("entries out of newest-first order at index %d: %q came after %q", i, order[i], order[i-1])
			}
		}
	})

	t.Run("a second organization's key never sees this organization's log entries", func(t *testing.T) {
		adminAuth := "Bearer " + support.AdminAPIKey
		var otherOrg organizationDTO
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Other Org"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create other org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &otherOrg); err != nil {
			t.Fatalf("decode other org: %v", err)
		}
		var otherKey issuedKeyDTO
		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+otherOrg.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("issue other org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &otherKey); err != nil {
			t.Fatalf("decode other key: %v", err)
		}

		page := fetchLogs(t, wired, "Bearer "+otherKey.Key, "?limit=100")

		if len(page.Entries) != 0 {
			t.Fatalf("org B sees %d entries, want 0 — org isolation violated", len(page.Entries))
		}
	})
}
