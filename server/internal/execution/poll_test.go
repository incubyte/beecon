// Package execution_test (poll half): exercises FetchTriggerRecords against
// hand-written fakes — reuses newFakeToolReader, fakeConnectionReader/
// activeConnectionReader, fakeProviderClient/fakeSequencedProviderClient,
// fixedClock, noOpSleep, rateLimitedResponse, testOrg/testUser/
// testConnectionID declared in facade_test.go (same package). Slice 4's own
// generic-mapping claim (PD28: "no Outlook/Hubspot-specific Go code") is
// proven by exercising two structurally different poll mappings — Outlook's
// GET+query+dotted-payload shape and Hubspot's POST+dotted-body shape —
// through the exact same FetchTriggerRecords code path.
package execution_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/execution"
)

const outlookPollTriggerSlug = "outlook-message-received"
const hubspotPollTriggerSlug = "hubspot-contact-created"

// fakeTriggerDefinitionReader is a hand-written execution.TriggerDefinitionReader:
// one registered trigger (provider + full poll mapping) per slug, or
// catalog.ErrTriggerDefinitionNotFound for anything else.
type fakeTriggerDefinitionReader struct {
	bySlug map[string]struct {
		provider catalog.ProviderDefinition
		trigger  catalog.TriggerDefinition
	}
}

func newFakeTriggerDefinitionReader(provider catalog.ProviderDefinition, trigger catalog.TriggerDefinition) fakeTriggerDefinitionReader {
	return fakeTriggerDefinitionReader{bySlug: map[string]struct {
		provider catalog.ProviderDefinition
		trigger  catalog.TriggerDefinition
	}{
		trigger.Slug: {provider: provider, trigger: trigger},
	}}
}

func (f fakeTriggerDefinitionReader) FindTriggerBySlug(_ context.Context, slug string) (catalog.ProviderDefinition, catalog.TriggerDefinition, error) {
	entry, ok := f.bySlug[slug]
	if !ok {
		return catalog.ProviderDefinition{}, catalog.TriggerDefinition{}, catalog.ErrTriggerDefinitionNotFound()
	}
	return entry.provider, entry.trigger, nil
}

// outlookPollDefinition mirrors outlook.yaml's real outlook-message-received
// poll mapping (PD35) exactly: GET, {config.folderId} path templating,
// {watermark} embedded in a query filter, recordsPath "value", and a
// dotted-path payload field (from.emailAddress.address).
func outlookPollDefinition() (catalog.ProviderDefinition, catalog.TriggerDefinition) {
	provider := catalog.ProviderDefinition{Slug: "outlook", BaseURL: "https://graph.microsoft.com/v1.0"}
	trigger := catalog.TriggerDefinition{
		Slug: outlookPollTriggerSlug,
		ConfigSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"folderId": map[string]any{"type": "string", "default": "Inbox"}},
		},
		Poll: catalog.TriggerPollMapping{
			Method:              "GET",
			Path:                "/me/mailFolders/{config.folderId}/messages",
			Query:               map[string]string{"$filter": "receivedDateTime gt {watermark}", "$orderby": "receivedDateTime"},
			RecordsPath:         "value",
			RecordIDPath:        "id",
			RecordTimestampPath: "receivedDateTime",
			Payload: map[string]string{
				"id": "id", "subject": "subject", "from": "from.emailAddress.address",
				"receivedDateTime": "receivedDateTime", "bodyPreview": "bodyPreview", "folderId": "parentFolderId",
			},
		},
	}
	return provider, trigger
}

func outlookMessageResponseBody() string {
	return `{"value":[{"id":"msg-1","subject":"Hello","from":{"emailAddress":{"address":"ada@example.com"}},"receivedDateTime":"2026-01-01T12:00:00Z","bodyPreview":"preview text","parentFolderId":"Inbox"}]}`
}

// hubspotPollDefinition mirrors hubspot.yaml's real hubspot-contact-created
// poll mapping (PD35): POST, a dotted-key JSON body (not query!) carrying
// {watermark}, recordsPath "results", and recordTimestampPath "createdAt" —
// a structurally different mapping shape from Outlook's, on purpose.
func hubspotPollDefinition() (catalog.ProviderDefinition, catalog.TriggerDefinition) {
	provider := catalog.ProviderDefinition{Slug: "hubspot", BaseURL: "https://api.hubapi.com"}
	trigger := catalog.TriggerDefinition{
		Slug:         hubspotPollTriggerSlug,
		ConfigSchema: map[string]any{"type": "object"},
		Poll: catalog.TriggerPollMapping{
			Method: "POST",
			Path:   "/crm/v3/objects/contacts/search",
			Body: map[string]string{
				"filterGroups.0.filters.0.propertyName": "createdate",
				"filterGroups.0.filters.0.operator":     "GT",
				"filterGroups.0.filters.0.value":        "{watermark}",
				"sorts.0":                               "createdate",
			},
			RecordsPath:         "results",
			RecordIDPath:        "id",
			RecordTimestampPath: "createdAt",
			Payload:             map[string]string{"id": "id", "properties": "properties"},
		},
	}
	return provider, trigger
}

func hubspotContactResponseBody() string {
	return `{"results":[{"id":"contact-1","createdAt":"2026-01-01T12:00:00Z","properties":{"email":"ada@example.com"}}]}`
}

func pollQuery(slug string) execution.PollQuery {
	return execution.PollQuery{
		OrgID: testOrg, UserID: testUser, ConnectionID: testConnectionID, TriggerSlug: slug, Config: map[string]any{},
	}
}

// --- Mapping evaluation: request building + record/payload extraction ---

func TestFetchTriggerRecords_EvaluatesTheOutlookMappingBuildingTheRequestAndExtractingRecordsAndPayload(t *testing.T) {
	provider, trigger := outlookPollDefinition()
	fakeProvider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: outlookMessageResponseBody()}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), fakeProvider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(provider, trigger))

	result, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fakeProvider.lastReq.URL, "/me/mailFolders/Inbox/messages") {
		t.Errorf("URL = %q, want baseUrl+path joined with the config default (Inbox) substituted", fakeProvider.lastReq.URL)
	}
	if fakeProvider.lastReq.Query["$filter"] == "" || strings.Contains(fakeProvider.lastReq.Query["$filter"], "{watermark}") {
		t.Errorf("query[$filter] = %q, want the {watermark} token substituted with a real timestamp", fakeProvider.lastReq.Query["$filter"])
	}
	if fakeProvider.lastReq.AccessToken != rawAccessToken {
		t.Errorf("AccessToken = %q, want the connection's own token", fakeProvider.lastReq.AccessToken)
	}
	if len(result.Records) != 1 {
		t.Fatalf("Records = %+v, want exactly 1", result.Records)
	}
	record := result.Records[0]
	if record.ID != "msg-1" {
		t.Errorf("ID = %q, want %q", record.ID, "msg-1")
	}
	wantTimestamp := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !record.Timestamp.Equal(wantTimestamp) {
		t.Errorf("Timestamp = %v, want %v", record.Timestamp, wantTimestamp)
	}
	if record.Payload["from"] != "ada@example.com" {
		t.Errorf(`Payload["from"] = %v, want %q (a dotted-path payload field)`, record.Payload["from"], "ada@example.com")
	}
	if record.Payload["subject"] != "Hello" {
		t.Errorf(`Payload["subject"] = %v, want %q`, record.Payload["subject"], "Hello")
	}
}

// TestFetchTriggerRecords_EvaluatesTheHubspotMappingProvingTheEngineIsGenericNotOutlookSpecific
// is Slice 4's own "definition-driven, not Outlook-specific" claim (PD28):
// the identical FetchTriggerRecords code path evaluates a structurally
// different mapping (POST, dotted body instead of query, a different
// recordTimestampPath) with no provider-specific Go code.
func TestFetchTriggerRecords_EvaluatesTheHubspotMappingProvingTheEngineIsGenericNotOutlookSpecific(t *testing.T) {
	provider, trigger := hubspotPollDefinition()
	fakeProvider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: hubspotContactResponseBody()}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), fakeProvider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(provider, trigger))

	result, err := f.FetchTriggerRecords(context.Background(), pollQuery(hubspotPollTriggerSlug))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeProvider.lastReq.Method != "" && fakeProvider.lastReq.Method != "POST" {
		// ToolCallRequest.Method isn't itself asserted by fakeProviderClient
		// beyond what's set on the request; buildPollRequest sets it from
		// poll.Method, so this is really pinning that Method arrived at all.
		t.Errorf("Method = %q, want %q", fakeProvider.lastReq.Method, "POST")
	}
	if !strings.Contains(fakeProvider.lastReq.Body, `"createdate"`) || !strings.Contains(fakeProvider.lastReq.Body, `"GT"`) {
		t.Errorf("Body = %q, want the dotted filterGroups.* keys nested into a JSON body", fakeProvider.lastReq.Body)
	}
	if strings.Contains(fakeProvider.lastReq.Body, "{watermark}") {
		t.Errorf("Body = %q, want the {watermark} token substituted with a real timestamp", fakeProvider.lastReq.Body)
	}
	if len(result.Records) != 1 || result.Records[0].ID != "contact-1" {
		t.Fatalf("Records = %+v, want exactly [contact-1]", result.Records)
	}
	properties, ok := result.Records[0].Payload["properties"].(map[string]any)
	if !ok || properties["email"] != "ada@example.com" {
		t.Errorf("Payload[properties] = %v, want the nested properties object carried through", result.Records[0].Payload["properties"])
	}
}

// --- Config defaults (PD35: folderId defaults to Inbox when the instance's
// own config omits it) ---

func TestFetchTriggerRecords_MergesTheConfigSchemasDefaultWhenTheQuerysConfigOmitsIt(t *testing.T) {
	provider, trigger := outlookPollDefinition()
	fakeProvider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"value":[]}`}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), fakeProvider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(provider, trigger))

	query := pollQuery(outlookPollTriggerSlug)
	query.Config = map[string]any{} // no folderId supplied at all

	if _, err := f.FetchTriggerRecords(context.Background(), query); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fakeProvider.lastReq.URL, "/mailFolders/Inbox/") {
		t.Errorf("URL = %q, want the configSchema's own folderId default (Inbox) applied", fakeProvider.lastReq.URL)
	}
}

func TestFetchTriggerRecords_AnExplicitConfigValueOverridesTheSchemasDefault(t *testing.T) {
	provider, trigger := outlookPollDefinition()
	fakeProvider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"value":[]}`}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), fakeProvider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(provider, trigger))

	query := pollQuery(outlookPollTriggerSlug)
	query.Config = map[string]any{"folderId": "Archive"}

	if _, err := f.FetchTriggerRecords(context.Background(), query); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(fakeProvider.lastReq.URL, "/mailFolders/Archive/") {
		t.Errorf("URL = %q, want the instance's own explicit folderId (Archive), not the schema default", fakeProvider.lastReq.URL)
	}
}

// --- Errors: unknown slug, non-ACTIVE connection, malformed response,
// provider error status ---

func TestFetchTriggerRecords_UnknownTriggerSlugIsNotFoundAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"value":[]}`}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(fakeTriggerDefinitionReader{})

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery("does-not-exist"))

	if err == nil {
		t.Fatal("expected an error for an unknown trigger slug, got nil")
	}
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0", provider.callCount)
	}
}

func TestFetchTriggerRecords_ANonActiveConnectionIsAValidationErrorAndNeverCallsTheProvider(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"value":[]}`}}
	connReader := &fakeConnectionReader{access: map[connections.ConnectionID]connections.ExecutionAccess{
		testConnectionID: {Status: connections.StatusExpired},
	}}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	de := assertDomainError(t, err, execution.CodeValidationFailed, http.StatusUnprocessableEntity)
	issue, _ := de.Details["issue"].(string)
	if !strings.Contains(issue, "EXPIRED") {
		t.Errorf("issue = %q, want it to name the connection's actual status", issue)
	}
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0 — a non-ACTIVE connection must never reach the provider", provider.callCount)
	}
}

func TestFetchTriggerRecords_AResponseMissingTheRecordsPathIsAnError(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"unexpected":"shape"}`}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	if err == nil {
		t.Fatal("expected an error when the response carries no records at recordsPath, got nil")
	}
}

func TestFetchTriggerRecords_AProviderErrorStatusSurfacesAsAPlainGoError(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 503, Body: "service unavailable"}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	if err == nil {
		t.Fatal("expected an error for a 503 upstream response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %q, want it to surface the provider's status code", err.Error())
	}
}

// --- PD21 rate-limit normalization + PD18 refresh-on-401 (inherited from
// the shared retryLoop/ConnectionReader path tool execution already uses) ---

func TestFetchTriggerRecords_ARateLimitedAttemptIsRetriedAndThenSucceeds(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("1"),
		{StatusCode: 200, Body: outlookMessageResponseBody()},
	}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).
		WithSleep(noOpSleep).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	result, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 (one rate-limited attempt, one retried success)", provider.callCount)
	}
	if len(result.Records) != 1 {
		t.Fatalf("Records = %+v, want exactly 1 once the retry succeeds", result.Records)
	}
}

func TestFetchTriggerRecords_RetriesExhaustedSurfacesTheRateLimitedError(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("2"), rateLimitedResponse("2"), rateLimitedResponse("2"),
	}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).
		WithSleep(noOpSleep).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	de := assertDomainError(t, err, execution.CodeRateLimited, http.StatusTooManyRequests)
	if de.Headers["Retry-After"] != "2" {
		t.Errorf(`Headers["Retry-After"] = %q, want %q`, de.Headers["Retry-After"], "2")
	}
	if provider.callCount != 3 {
		t.Errorf("provider was called %d times, want exactly 3 (PD21's retry ceiling)", provider.callCount)
	}
}

// TestFetchTriggerRecords_A401TriggersExactlyOneRefreshAndOneRetriedCallThatSucceeds
// is PD18's inherited reactive refresh path (execution/facade.go's own
// fetchPollResponseAfterRefresh): an expired stored token surfaces as a 401,
// triggering exactly one refresh and one retried call.
func TestFetchTriggerRecords_A401TriggersExactlyOneRefreshAndOneRetriedCallThatSucceeds(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 401, Body: "unauthorized"},
		{StatusCode: 200, Body: outlookMessageResponseBody()},
	}}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "stale-access-token"},
		},
		refreshAccess: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "refreshed-access-token"},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	result, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connReader.refreshCallCount != 1 {
		t.Errorf("refreshCallCount = %d, want exactly 1", connReader.refreshCallCount)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 (the original 401 plus one retry)", provider.callCount)
	}
	if provider.requests[1].AccessToken != "refreshed-access-token" {
		t.Errorf("retried request's AccessToken = %q, want the freshly refreshed token", provider.requests[1].AccessToken)
	}
	if len(result.Records) != 1 {
		t.Fatalf("Records = %+v, want exactly 1 once the retried call succeeds", result.Records)
	}
}

// TestFetchTriggerRecords_WhenTheRefreshedConnectionIsNoLongerActiveReturnsAValidationErrorWithoutRetrying
// mirrors Execute's own AC9 coverage: a refresh that leaves the connection
// no longer ACTIVE must never retry the call.
func TestFetchTriggerRecords_WhenTheRefreshedConnectionIsNoLongerActiveReturnsAValidationErrorWithoutRetrying(t *testing.T) {
	providerDef, trigger := outlookPollDefinition()
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 401, Body: "unauthorized"},
	}}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "stale-access-token"},
		},
		refreshAccess: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusExpired},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now())).
		WithTriggerDefinitions(newFakeTriggerDefinitionReader(providerDef, trigger))

	_, err := f.FetchTriggerRecords(context.Background(), pollQuery(outlookPollTriggerSlug))

	de := assertDomainError(t, err, execution.CodeValidationFailed, http.StatusUnprocessableEntity)
	issue, _ := de.Details["issue"].(string)
	if !strings.Contains(issue, "EXPIRED") {
		t.Errorf("issue = %q, want it to explain the connection's actual status", issue)
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1 — a connection left non-ACTIVE by the refresh must never be retried", provider.callCount)
	}
}
