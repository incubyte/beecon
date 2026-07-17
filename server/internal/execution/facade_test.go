// Package execution_test exercises execution.Facade.Execute against
// hand-written fakes for its four narrow ports (ToolReader, ConnectionReader,
// ProviderClient, Recorder) — Slice 5's AC1-AC8, with extra depth per the
// HIGH-risk callout: the provider must provably never be called on a
// platform-level or tool-level short-circuit, and the raw access token must
// never leak into a Result or its error message.
package execution_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/execution"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const (
	testOrg          = organizations.OrgID("org_1")
	otherOrg         = organizations.OrgID("org_2")
	testUser         = organizations.UserID("user_1")
	otherUser        = organizations.UserID("user_2")
	testConnectionID = connections.ConnectionID("conn_1")
	testToolSlug     = "outlook-list-messages"
	rawAccessToken   = "raw-microsoft-access-token-value"
)

func testTool() catalog.ProviderTool {
	return catalog.ProviderTool{
		Slug:   testToolSlug,
		Method: "GET",
		Path:   "https://graph.microsoft.com/v1.0/me/messages",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"top":    map[string]any{"type": "integer"},
				"skip":   map[string]any{"type": "integer"},
				"select": map[string]any{"type": "string"},
				"filter": map[string]any{"type": "string"},
			},
		},
	}
}

// fakeToolReader is a hand-written execution.ToolReader: a single registered
// tool, or ErrToolNotFound for anything else (AC3).
type fakeToolReader struct {
	tools map[string]catalog.ProviderTool
}

func newFakeToolReader() fakeToolReader {
	return fakeToolReader{tools: map[string]catalog.ProviderTool{testToolSlug: testTool()}}
}

func (f fakeToolReader) FindToolBySlug(_ context.Context, slug string) (catalog.ProviderDefinition, catalog.ProviderTool, error) {
	tool, ok := f.tools[slug]
	if !ok {
		return catalog.ProviderDefinition{}, catalog.ProviderTool{}, catalog.ErrToolNotFound()
	}
	return catalog.ProviderDefinition{Slug: "outlook"}, tool, nil
}

// fakeConnectionReader is a hand-written execution.ConnectionReader: scripted
// per connection id, so tests can model an ACTIVE connection (with a token),
// a non-ACTIVE one, or an unknown/cross-org/cross-user one (via
// notFoundConnections) without a real connections.Facade.
type fakeConnectionReader struct {
	access            map[connections.ConnectionID]connections.ExecutionAccess
	notFoundIDs       map[connections.ConnectionID]bool
	resolveCallCount  int
	lastResolvedOrg   organizations.OrgID
	lastResolvedUser  organizations.UserID
	lastResolvedConID connections.ConnectionID

	// refreshAccess/refreshErr script RefreshForExecution's own result
	// (Slice 4's PD18 retry path) independently of ResolveForExecution's —
	// left unset, RefreshForExecution falls back to whatever access map
	// entry ResolveForExecution would have returned, so a test that doesn't
	// care about refresh at all still exercises a 401-then-retry with the
	// same scripted response.
	refreshAccess    map[connections.ConnectionID]connections.ExecutionAccess
	refreshErr       error
	refreshCallCount int
}

func (f *fakeConnectionReader) ResolveForExecution(_ context.Context, org organizations.OrgID, userID organizations.UserID, id connections.ConnectionID) (connections.ExecutionAccess, error) {
	f.resolveCallCount++
	f.lastResolvedOrg = org
	f.lastResolvedUser = userID
	f.lastResolvedConID = id
	if f.notFoundIDs[id] {
		return connections.ExecutionAccess{}, connections.ErrNotFound()
	}
	access, ok := f.access[id]
	if !ok {
		return connections.ExecutionAccess{}, connections.ErrNotFound()
	}
	return access, nil
}

func (f *fakeConnectionReader) RefreshForExecution(_ context.Context, _ organizations.OrgID, _ organizations.UserID, id connections.ConnectionID) (connections.ExecutionAccess, error) {
	f.refreshCallCount++
	if f.notFoundIDs[id] {
		return connections.ExecutionAccess{}, connections.ErrNotFound()
	}
	if f.refreshErr != nil {
		return connections.ExecutionAccess{}, f.refreshErr
	}
	if access, ok := f.refreshAccess[id]; ok {
		return access, nil
	}
	access, ok := f.access[id]
	if !ok {
		return connections.ExecutionAccess{}, connections.ErrNotFound()
	}
	return access, nil
}

func activeConnectionReader() *fakeConnectionReader {
	return &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: rawAccessToken},
		},
	}
}

// fakeProviderClient is a hand-written execution.ProviderClient: scripted to
// return one response (or one network error) and records the request it
// received, so tests can assert on forwarded query parameters and the
// carried access token, and prove it was never called at all.
type fakeProviderClient struct {
	response  execution.ToolCallResponse
	err       error
	callCount int
	lastReq   execution.ToolCallRequest
}

func (f *fakeProviderClient) Call(_ context.Context, req execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	f.callCount++
	f.lastReq = req
	if f.err != nil {
		return execution.ToolCallResponse{}, f.err
	}
	return f.response, nil
}

func messagesResponse() execution.ToolCallResponse {
	return execution.ToolCallResponse{
		StatusCode: 200,
		Body:       `{"value":[{"id":"msg-1","subject":"Hello"}]}`,
	}
}

// fakeSequencedProviderClient returns one scripted ToolCallResponse per call,
// in order (and repeats the last one if called more times than scripted) —
// Slice 4's 401-then-retry path needs a provider that can behave differently
// on its first call and its retried call, which fakeProviderClient's single
// fixed response can't express.
type fakeSequencedProviderClient struct {
	responses []execution.ToolCallResponse
	callCount int
	requests  []execution.ToolCallRequest
}

func (f *fakeSequencedProviderClient) Call(_ context.Context, req execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	f.requests = append(f.requests, req)
	index := f.callCount
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	f.callCount++
	return f.responses[index], nil
}

// fakeRecorder is a hand-written execution.Recorder: records every entry
// handed to it, so tests can assert AC8's log-entry shape.
type fakeRecorder struct {
	entries []execution.LogEntry
	err     error
}

func (f *fakeRecorder) Record(_ context.Context, entry execution.LogEntry) error {
	f.entries = append(f.entries, entry)
	return f.err
}

func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) *httpx.DomainError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Fatalf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Fatalf("error status = %d, want %d", de.Status, wantStatus)
	}
	return de
}

// --- AC1: happy path ---

func TestExecute_HappyPathReturnsSuccessfulResultWithProviderData(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"top": float64(5)})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	if result.Error != nil {
		t.Errorf("Error = %+v, want nil", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data = %T, want a decoded JSON object", result.Data)
	}
	if _, present := data["value"]; !present {
		t.Errorf("Data %+v does not carry the mailbox messages under \"value\"", data)
	}
}

// TestExecute_ForwardsArgumentsAsProviderQueryParameters is AC1's other half:
// top/skip/select/filter must reach the provider call as query parameters.
func TestExecute_ForwardsArgumentsAsProviderQueryParameters(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{
		"top": float64(10), "skip": float64(5), "select": "subject", "filter": "isRead eq false",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"top": "10", "skip": "5", "select": "subject", "filter": "isRead eq false"}
	for key, wantValue := range want {
		if got := provider.lastReq.Query[key]; got != wantValue {
			t.Errorf("query[%q] = %q, want %q", key, got, wantValue)
		}
	}
}

func TestExecute_CarriesTheConnectionsAccessTokenAsTheBearerToken(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.lastReq.AccessToken != rawAccessToken {
		t.Errorf("AccessToken = %q, want the connection's own token %q", provider.lastReq.AccessToken, rawAccessToken)
	}
}

// --- Mapping (Phase 2 Slice 1, PD13): declared header mapping ---

// toolWithHeaderMapping is testTool() plus a declared header mapping, so
// tests can prove buildToolHeaders actually reaches the provider request
// rather than just parsing (the input schema declares no
// additionalProperties, so "preference" is accepted alongside top/skip/
// select/filter).
func toolWithHeaderMapping() catalog.ProviderTool {
	tool := testTool()
	tool.Mapping = catalog.Mapping{Header: map[string]string{"Prefer": "{input.preference}"}}
	return tool
}

func fakeToolReaderWithTool(tool catalog.ProviderTool) fakeToolReader {
	return fakeToolReader{tools: map[string]catalog.ProviderTool{testToolSlug: tool}}
}

func TestExecute_ForwardsADeclaredHeaderMappingValueToTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithHeaderMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"preference": "return=minimal"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := provider.lastReq.Headers["Prefer"]; got != "return=minimal" {
		t.Errorf("Headers[%q] = %q, want %q", "Prefer", got, "return=minimal")
	}
}

// TestExecute_OmitsAHeaderMappingEntryWhenItsInputIsNotSupplied is the other
// half: buildToolHeaders must not send an empty or literal "{input.x}" value
// for an optional argument the caller left out.
func TestExecute_OmitsAHeaderMappingEntryWhenItsInputIsNotSupplied(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithHeaderMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := provider.lastReq.Headers["Prefer"]; present {
		t.Errorf("Headers[%q] = %q, want the header omitted entirely when its input was not supplied", "Prefer", provider.lastReq.Headers["Prefer"])
	}
}

// --- Slice 1 (Gap A, PD13): composable/embedded mapping values ---

// toolWithEmbeddedQueryMapping declares a query mapping value that embeds a
// token inside a larger literal, mirroring the Outlook OData-filter shape
// the spec anchors on ("receivedDateTime gt {input.since}").
func toolWithEmbeddedQueryMapping() catalog.ProviderTool {
	tool := testTool()
	tool.Mapping = catalog.Mapping{Query: map[string]string{"filter": "receivedDateTime gt {input.since}"}}
	return tool
}

// toolWithEmbeddedBodyMapping declares a dotted-key body mapping whose value
// embeds a token, mirroring hubspot-create-contact's "properties.email"
// shape but with the value composed rather than a bare whole token.
func toolWithEmbeddedBodyMapping() catalog.ProviderTool {
	tool := testTool()
	tool.Mapping = catalog.Mapping{Body: map[string]string{"properties.email": "mailto:{input.email}"}}
	return tool
}

// TestExecute_AnEmbeddedQueryMappingValueRendersWithTheTokenSubstitutedInPlace
// proves an embedded query mapping value reaches the provider substituted,
// not sent verbatim with its braces intact.
func TestExecute_AnEmbeddedQueryMappingValueRendersWithTheTokenSubstitutedInPlace(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithEmbeddedQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"since": "2024-01-01T00:00:00Z"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	want := "receivedDateTime gt 2024-01-01T00:00:00Z"
	if got := provider.lastReq.Query["filter"]; got != want {
		t.Errorf("Query[%q] = %q, want %q", "filter", got, want)
	}
}

// TestExecute_ADottedKeyBodyMappingWithAnEmbeddedValueNestsTheComposedValue
// proves the dotted-key nesting (properties.email) still builds correctly
// once the mapped value itself is a composed, not whole-token, expression.
func TestExecute_ADottedKeyBodyMappingWithAnEmbeddedValueNestsTheComposedValue(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithEmbeddedBodyMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"email": "ada@example.com"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(provider.lastReq.Body), &body); err != nil {
		t.Fatalf("could not decode provider request body %q as JSON: %v", provider.lastReq.Body, err)
	}
	properties, ok := body["properties"].(map[string]any)
	if !ok {
		t.Fatalf("body %+v does not carry a nested \"properties\" object", body)
	}
	if got := properties["email"]; got != "mailto:ada@example.com" {
		t.Errorf(`properties["email"] = %v, want %q`, got, "mailto:ada@example.com")
	}
}

// TestExecute_AnEmbeddedMappingValueWithAMissingTokenFailsInvalidArgumentsAndNeverCallsTheProvider
// is AC7's tool-level guarantee: a query/header/body mapping value that
// embeds a token the call omitted must fail the whole tool call as
// invalid-arguments naming the missing token, and the provider is provably
// never called — the request is never even partially built.
func TestExecute_AnEmbeddedMappingValueWithAMissingTokenFailsInvalidArgumentsAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithEmbeddedQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v (an embedded missing token is a tool-level failure, not an HTTP error)", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false when an embedded mapping token is not supplied")
	}
	if result.Error == nil || result.Error.Code != execution.CodeInvalidArguments {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeInvalidArguments)
	}
	if !strings.Contains(result.Error.Message, "{input.since}") {
		t.Errorf("Error.Message = %q, want it to name the missing token %q", result.Error.Message, "{input.since}")
	}
	if provider.callCount != 0 {
		t.Fatalf("provider was called %d times, want 0 — an embedded missing token must never reach the provider", provider.callCount)
	}
}

// --- AC2: invalid arguments ---

func TestExecute_InvalidArgumentsReturnFailureResultAndNeverCallTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"top": "not-a-number"})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v (AC2 is a tool-level failure, not an HTTP error)", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for invalid arguments")
	}
	if result.Error == nil || result.Error.Code != execution.CodeInvalidArguments {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeInvalidArguments)
	}
	if result.Data != nil {
		t.Errorf("Data = %v, want nil", result.Data)
	}
	if provider.callCount != 0 {
		t.Fatalf("provider was called %d times, want 0 — invalid arguments must never reach the provider", provider.callCount)
	}
}

// --- AC3: unknown tool slug ---

func TestExecute_UnknownToolSlugReturnsNotFoundAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	connReader := activeConnectionReader()
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, "unknown-tool-slug", map[string]any{})

	assertDomainError(t, err, "not_found", 404)
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0", provider.callCount)
	}
	if connReader.resolveCallCount != 0 {
		t.Errorf("connection lookup was called %d times, want 0 — an unknown tool slug must short-circuit before resolving the connection", connReader.resolveCallCount)
	}
}

// --- AC4: non-ACTIVE connection ---

func TestExecute_NonActiveConnectionReturnsFailureResultWithStatusExplainingErrorAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusInitiated},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for a non-ACTIVE connection")
	}
	if result.Error == nil || result.Error.Code != execution.CodeConnectionNotActive {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeConnectionNotActive)
	}
	if !strings.Contains(result.Error.Message, "INITIATED") {
		t.Errorf("Error.Message = %q, want it to explain the connection's actual status", result.Error.Message)
	}
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0 — a non-ACTIVE connection must never reach the provider", provider.callCount)
	}
}

// --- AC5: cross-org connection ---

func TestExecute_ConnectionBelongingToAnotherOrganizationReturnsNotFoundAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	connReader := &fakeConnectionReader{notFoundIDs: map[connections.ConnectionID]bool{testConnectionID: true}}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), otherOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	assertDomainError(t, err, "not_found", 404)
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0", provider.callCount)
	}
}

// --- AC6: connectionId not belonging to userId ---

func TestExecute_ConnectionNotBelongingToTheGivenUserIDReturnsErrorAndNeverCallsTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	// The fake models "belongs to a different user" the same way
	// connections.ResolveForExecution does: indistinguishable from not-found.
	connReader := &fakeConnectionReader{notFoundIDs: map[connections.ConnectionID]bool{testConnectionID: true}}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, otherUser, testConnectionID, testToolSlug, map[string]any{})

	assertDomainError(t, err, "not_found", 404)
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0 — a connectionId that does not belong to userID must never reach the provider", provider.callCount)
	}
}

// --- AC7: upstream provider errors ---

func TestExecute_Upstream4xxReturnsFailureResultSurfacingStatusAndMessage(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 401, Body: `{"error":"invalid_token"}`}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for a 401 upstream response")
	}
	if result.Error == nil || result.Error.Code != execution.CodeProviderError {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeProviderError)
	}
	if !strings.Contains(result.Error.Message, "401") {
		t.Errorf("Error.Message = %q, want it to surface the provider's status code", result.Error.Message)
	}
	if !strings.Contains(result.Error.Message, "invalid_token") {
		t.Errorf("Error.Message = %q, want it to surface the provider's response body", result.Error.Message)
	}
	if result.Data != nil {
		t.Errorf("Data = %v, want nil for a failed execution", result.Data)
	}
}

func TestExecute_Upstream5xxReturnsFailureResultSurfacingStatusAndMessage(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 503, Body: "service unavailable"}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for a 503 upstream response")
	}
	if result.Error == nil || result.Error.Code != execution.CodeProviderError {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeProviderError)
	}
	if !strings.Contains(result.Error.Message, "503") {
		t.Errorf("Error.Message = %q, want it to surface the provider's status code", result.Error.Message)
	}
}

func TestExecute_ProviderUnreachableReturnsFailureResultWithoutLeakingTheTransportError(t *testing.T) {
	provider := &fakeProviderClient{err: errors.New("dial tcp: connection refused")}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false when the provider cannot be reached")
	}
	if result.Error == nil || result.Error.Code != execution.CodeProviderUnavailable {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeProviderUnavailable)
	}
}

// --- AC8: recording ---

func TestExecute_ASuccessfulExecutionWritesALogEntryWithOrgUserConnectionToolAndStatus(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	recorder := &fakeRecorder{}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := now
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, func() time.Time {
		got := clock
		clock = clock.Add(250 * time.Millisecond) // second call (duration end) advances the clock
		return got
	})

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1", len(recorder.entries))
	}
	entry := recorder.entries[0]
	if entry.OrgID != testOrg {
		t.Errorf("OrgID = %q, want %q", entry.OrgID, testOrg)
	}
	if entry.UserID != testUser {
		t.Errorf("UserID = %q, want %q", entry.UserID, testUser)
	}
	if entry.ConnectionID != testConnectionID {
		t.Errorf("ConnectionID = %q, want %q", entry.ConnectionID, testConnectionID)
	}
	if entry.ToolSlug != testToolSlug {
		t.Errorf("ToolSlug = %q, want %q", entry.ToolSlug, testToolSlug)
	}
	if entry.Status != 200 {
		t.Errorf("Status = %d, want %d", entry.Status, 200)
	}
	if entry.DurationMs <= 0 {
		t.Errorf("DurationMs = %d, want > 0", entry.DurationMs)
	}
}

// TestExecute_AFailedUpstreamCallStillWritesALogEntryWithItsStatus uses a
// non-retriable status (500), not 429: since Slice 6 (PD21) normalizes and
// retries a 429 upstream response (see the "Slice 6" tests below), a bare 429
// is no longer a one-shot upstream failure — this AC8 test only needs to
// prove a failed-but-not-rate-limited call still writes exactly one log entry
// carrying its status.
func TestExecute_AFailedUpstreamCallStillWritesALogEntryWithItsStatus(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 500, Body: "internal server error"}}
	recorder := &fakeRecorder{}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1", len(recorder.entries))
	}
	if recorder.entries[0].Status != 500 {
		t.Errorf("Status = %d, want %d", recorder.entries[0].Status, 500)
	}
}

func TestExecute_ANilRecorderIsASilentNoOp(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatal("expected a successful execution even with no recorder wired")
	}
}

func TestExecute_ARecorderErrorDoesNotFailTheExecutionItself(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	recorder := &fakeRecorder{err: errors.New("logging backend unavailable")}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v (logging is observability, not a precondition of the primary operation)", err)
	}
	if !result.Successful {
		t.Fatal("expected the tool execution to still succeed despite the recorder failing")
	}
}

// --- HIGH-risk: the raw access token must never leak ---

func TestExecute_TheRawAccessTokenNeverAppearsInTheSuccessfulResult(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data, ok := result.Data.(string); ok && strings.Contains(data, rawAccessToken) {
		t.Fatalf("Data contains the raw access token: %v", result.Data)
	}
}

// --- Slice 4: PD18's reactive refresh path (a provider 401 triggers exactly
// one refresh, then exactly one retried call) ---

func TestExecute_A401ResponseTriggersExactlyOneRefreshAndOneRetriedCallThatSucceeds(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 401, Body: `{"error":"invalid_token"}`},
		messagesResponse(),
	}}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "stale-access-token"},
		},
		refreshAccess: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "refreshed-access-token"},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true after a successful retry; Error = %+v", result.Error)
	}
	if connReader.refreshCallCount != 1 {
		t.Errorf("refreshCallCount = %d, want exactly 1", connReader.refreshCallCount)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 (the original 401 plus one retry)", provider.callCount)
	}
}

// TestExecute_TheRetriedCallUsesTheRefreshedAccessToken proves the retry
// carries the connection's newly refreshed token, not the stale one that
// triggered the 401.
func TestExecute_TheRetriedCallUsesTheRefreshedAccessToken(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 401, Body: "unauthorized"},
		messagesResponse(),
	}}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "stale-access-token"},
		},
		refreshAccess: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "refreshed-access-token"},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	if _, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(provider.requests) != 2 {
		t.Fatalf("provider received %d requests, want 2", len(provider.requests))
	}
	if provider.requests[0].AccessToken != "stale-access-token" {
		t.Errorf("first request's AccessToken = %q, want the original stale token", provider.requests[0].AccessToken)
	}
	if provider.requests[1].AccessToken != "refreshed-access-token" {
		t.Errorf("retried request's AccessToken = %q, want the freshly refreshed token", provider.requests[1].AccessToken)
	}
}

// TestExecute_ANon401UpstreamErrorNeverTriggersARefresh is the coder's own
// flagged concern: only a 401 triggers PD18's reactive refresh.
func TestExecute_ANon401UpstreamErrorNeverTriggersARefresh(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 500, Body: "internal server error"},
	}}
	connReader := activeConnectionReader()
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for a 500 upstream response")
	}
	if connReader.refreshCallCount != 0 {
		t.Errorf("refreshCallCount = %d, want 0 — only a 401 may trigger a reactive refresh", connReader.refreshCallCount)
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1 — a non-401 error must never be retried", provider.callCount)
	}
}

// TestExecute_WhenTheRefreshedConnectionIsNoLongerActiveReturnsTheStatusExplainingFailureWithoutRetrying
// is AC9's execution-facing half: a refresh that leaves the connection
// EXPIRED (e.g. a revoked refresh token) must report the same status-
// explaining tool-level failure a non-ACTIVE connection produces up front,
// and must never retry the call.
func TestExecute_WhenTheRefreshedConnectionIsNoLongerActiveReturnsTheStatusExplainingFailureWithoutRetrying(t *testing.T) {
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
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false when the refresh leaves the connection EXPIRED")
	}
	if result.Error == nil || result.Error.Code != execution.CodeConnectionNotActive {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeConnectionNotActive)
	}
	if !strings.Contains(result.Error.Message, "EXPIRED") {
		t.Errorf("Error.Message = %q, want it to explain the connection's actual status", result.Error.Message)
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1 — a connection left non-ACTIVE by the refresh must never be retried", provider.callCount)
	}
}

// TestExecute_RetriesAtMostOnceEvenWhenTheRetriedCallAlsoReturns401 proves
// there is no second refresh and no loop: the retried call's own 401 is
// surfaced as a normal upstream error.
func TestExecute_RetriesAtMostOnceEvenWhenTheRetriedCallAlsoReturns401(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		{StatusCode: 401, Body: "unauthorized"},
		{StatusCode: 401, Body: "still unauthorized"},
	}}
	connReader := &fakeConnectionReader{
		access: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "stale-access-token"},
		},
		refreshAccess: map[connections.ConnectionID]connections.ExecutionAccess{
			testConnectionID: {Status: connections.StatusActive, AccessToken: "refreshed-access-token"},
		},
	}
	f := execution.NewFacade(newFakeToolReader(), connReader, provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false — the retried call's own 401 must surface as a failure")
	}
	if result.Error == nil || result.Error.Code != execution.CodeProviderError {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeProviderError)
	}
	if connReader.refreshCallCount != 1 {
		t.Errorf("refreshCallCount = %d, want exactly 1 — a second 401 on the retry must never trigger a second refresh", connReader.refreshCallCount)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 (no further retries)", provider.callCount)
	}
}

func TestExecute_TheRawAccessTokenNeverAppearsInAFailureResultsErrorMessage(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 401, Body: "unauthorized"}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != nil && strings.Contains(result.Error.Message, rawAccessToken) {
		t.Fatalf("Error.Message contains the raw access token: %q", result.Error.Message)
	}
}

// --- Slice 6 (PD21, ADR-0009): rate-limit normalization + platform retry ---

// noOpSleep is the sleepFunc these tests drive the retry loop with: it never
// actually waits, so a scripted rate-limit-then-retry never makes a test
// slow or flaky on timing.
func noOpSleep(_ context.Context, _ time.Duration) error { return nil }

// rateLimitedResponse is a normalized-rate-limit ToolCallResponse (a bare
// HTTP 429) carrying retryAfter verbatim as its Retry-After header value;
// empty exercises retry.go's jittered-backoff fallback instead.
func rateLimitedResponse(retryAfter string) execution.ToolCallResponse {
	return execution.ToolCallResponse{StatusCode: http.StatusTooManyRequests, Body: "{}", RetryAfter: retryAfter}
}

// AC1/AC2: a rate-limited attempt that then succeeds surfaces as a normal
// successful envelope, with no rate-limit detail (status code, "rate_limited")
// leaked anywhere in it.
func TestExecute_ARateLimitedAttemptThatThenSucceedsReturnsANormalSuccessfulEnvelopeWithNoLeakedDetail(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("1"),
		messagesResponse(),
	}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).WithSleep(noOpSleep)

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true once the retried call succeeds; Error = %+v", result.Error)
	}
	if provider.callCount != 2 {
		t.Errorf("provider was called %d times, want exactly 2 (one rate-limited attempt, one retried success)", provider.callCount)
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("marshal result: %v", marshalErr)
	}
	if strings.Contains(string(raw), "rate_limited") || strings.Contains(string(raw), "429") {
		t.Fatalf("successful envelope %s leaks rate-limit detail", raw)
	}
}

// AC3: retries exhausted (every attempt stayed rate-limited) is reported as a
// platform-level *httpx.DomainError — the carve-out the HTTP handler renders
// as 429 with a Retry-After header — never as a tool-level Result.
func TestExecute_RetriesExhaustedReturnsARateLimitedDomainErrorWithARetryAfterHeader(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("2"), rateLimitedResponse("2"), rateLimitedResponse("2"),
	}}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).WithSleep(noOpSleep)

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	de := assertDomainError(t, err, execution.CodeRateLimited, http.StatusTooManyRequests)
	if de.Headers["Retry-After"] != "2" {
		t.Errorf(`Headers["Retry-After"] = %q, want %q`, de.Headers["Retry-After"], "2")
	}
	if provider.callCount != 3 {
		t.Errorf("provider was called %d times, want exactly 3 (PD21's retry ceiling)", provider.callCount)
	}
}

// AC4: a non-retriable upstream status (400, 404, ...) is never retried and
// surfaces once as the usual tool-level failure.
func TestExecute_NonRetriableUpstreamStatusesAreNotRetriedAndSurfaceOnce(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound} {
		t.Run(fmt.Sprintf("status %d", status), func(t *testing.T) {
			provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: status, Body: "not a rate limit"}}
			f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now())).WithSleep(noOpSleep)

			result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

			if err != nil {
				t.Fatalf("unexpected platform-level error: %v", err)
			}
			if result.Successful {
				t.Fatal("Successful = true, want false for a non-retriable upstream error")
			}
			if result.Error == nil || result.Error.Code != execution.CodeProviderError {
				t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeProviderError)
			}
			if provider.callCount != 1 {
				t.Errorf("provider was called %d times, want exactly 1 — a non-retriable status must never be retried", provider.callCount)
			}
		})
	}
}

// AC5: every upstream attempt — including rate-limited ones — writes its own
// log entry, marked RateLimited where IsRateLimited normalized it as one.
func TestExecute_EveryRateLimitedAttemptWritesItsOwnLogEntryMarkedRateLimited(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("1"),
		messagesResponse(),
	}}
	recorder := &fakeRecorder{}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, fixedClock(time.Now())).WithSleep(noOpSleep)

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recorder.entries) != 2 {
		t.Fatalf("recorded %d entries, want exactly 2 (one per attempt)", len(recorder.entries))
	}
	if !recorder.entries[0].RateLimited || recorder.entries[0].Status != http.StatusTooManyRequests {
		t.Errorf("entries[0] = %+v, want RateLimited=true, Status=429", recorder.entries[0])
	}
	if recorder.entries[1].RateLimited || recorder.entries[1].Status != http.StatusOK {
		t.Errorf("entries[1] = %+v, want RateLimited=false, Status=200", recorder.entries[1])
	}
}

// TestExecute_ExhaustedRetriesLogsEveryAttemptAsRateLimited is AC5's
// exhaustion half: even though the platform-level error carve-out means
// Execute itself returns an error rather than a Result, every attempt the
// retry loop made along the way still wrote its own marked log entry.
func TestExecute_ExhaustedRetriesLogsEveryAttemptAsRateLimited(t *testing.T) {
	provider := &fakeSequencedProviderClient{responses: []execution.ToolCallResponse{
		rateLimitedResponse("1"), rateLimitedResponse("1"), rateLimitedResponse("1"),
	}}
	recorder := &fakeRecorder{}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, fixedClock(time.Now())).WithSleep(noOpSleep)

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	assertDomainError(t, err, execution.CodeRateLimited, http.StatusTooManyRequests)
	if len(recorder.entries) != 3 {
		t.Fatalf("recorded %d entries, want exactly 3 (one per exhausted attempt)", len(recorder.entries))
	}
	for i, entry := range recorder.entries {
		if !entry.RateLimited {
			t.Errorf("entries[%d].RateLimited = false, want true", i)
		}
	}
}

// --- Slice 2 (Gap C): tool-input defaults ---

// toolWithUserIdDefault builds on testTool() with an extra "userId" property
// declaring a schema default of "me" (mirroring the spec's own example),
// wired into the request the way callerBuildsMapping asks for (query, header,
// or path) — the one thing that differs test to test is where the defaulted
// value needs to actually show up in the provider's request.
func toolWithUserIdDefault(configure func(tool *catalog.ProviderTool)) catalog.ProviderTool {
	tool := testTool()
	properties := tool.InputSchema["properties"].(map[string]any)
	properties["userId"] = map[string]any{"type": "string", "default": "me"}
	configure(&tool)
	return tool
}

func toolWithUserIdDefaultQueryMapping() catalog.ProviderTool {
	return toolWithUserIdDefault(func(tool *catalog.ProviderTool) {
		tool.Mapping = catalog.Mapping{Query: map[string]string{"userId": "{input.userId}"}}
	})
}

// TestExecute_FillsAMissingToolArgumentWithItsSchemaDefaultAndForwardsItToTheProvider
// is Gap C's core behavior: an omitted argument whose schema declares a
// default reaches the provider carrying that default value.
func TestExecute_FillsAMissingToolArgumentWithItsSchemaDefaultAndForwardsItToTheProvider(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithUserIdDefaultQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	if got := provider.lastReq.Query["userId"]; got != "me" {
		t.Errorf(`Query["userId"] = %q, want the schema default %q`, got, "me")
	}
}

// TestExecute_AnExplicitlySuppliedArgumentIsNotOverriddenByItsSchemaDefault
// proves a caller-supplied value always wins over its schema default.
func TestExecute_AnExplicitlySuppliedArgumentIsNotOverriddenByItsSchemaDefault(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithUserIdDefaultQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"userId": "someone-else"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := provider.lastReq.Query["userId"]; got != "someone-else" {
		t.Errorf(`Query["userId"] = %q, want the caller's own value %q, not the schema default`, got, "someone-else")
	}
}

// TestExecute_AnExplicitNullArgumentCountsAsPresentAndIsNotDefaultedEvenThoughItFailsTypeValidation
// proves an explicit null counts as "supplied" for default-merging purposes:
// since it is not overridden by the "me" default, it is left as JSON null
// against a "type": "string" property, which the shared schema validator
// correctly rejects. If the default had wrongly filled a null value, this
// call would have succeeded instead.
func TestExecute_AnExplicitNullArgumentCountsAsPresentAndIsNotDefaultedEvenThoughItFailsTypeValidation(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithUserIdDefaultQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"userId": nil})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false — an explicit null must not be replaced by the schema default, and null fails the property's string type")
	}
	if result.Error == nil || result.Error.Code != execution.CodeInvalidArguments {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeInvalidArguments)
	}
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0", provider.callCount)
	}
}

// TestExecute_AnExplicitEmptyStringArgumentCountsAsPresentAndIsNotOverriddenByItsDefault
// is AC3's other case: an explicit empty string is valid against "type":
// "string", so the call succeeds — but the provider must receive the
// caller's own empty string, not the "me" default.
func TestExecute_AnExplicitEmptyStringArgumentCountsAsPresentAndIsNotOverriddenByItsDefault(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(toolWithUserIdDefaultQueryMapping()), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"userId": ""})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	if got, present := provider.lastReq.Query["userId"]; !present || got != "" {
		t.Errorf(`Query["userId"] = %q (present=%v), want the caller's own empty string, not the schema default %q`, got, present, "me")
	}
}

// TestExecute_DefaultsAreMergedBeforeValidationSoARequiredButDefaultedArgumentValidatesAndRuns
// is AC4: a tool call that omits a required-but-defaulted argument must
// validate and run rather than failing invalid-arguments — proving the merge
// happens before validateArguments, not after.
func TestExecute_DefaultsAreMergedBeforeValidationSoARequiredButDefaultedArgumentValidatesAndRuns(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	tool := toolWithUserIdDefaultQueryMapping()
	tool.InputSchema["required"] = []any{"userId"}
	f := execution.NewFacade(fakeToolReaderWithTool(tool), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true — a required-but-defaulted argument must validate and run; Error = %+v", result.Error)
	}
	if result.Error != nil && result.Error.Code == execution.CodeInvalidArguments {
		t.Fatalf("Error = %+v, want no invalid-arguments failure once the default fills the required property", result.Error)
	}
	if got := provider.lastReq.Query["userId"]; got != "me" {
		t.Errorf(`Query["userId"] = %q, want the schema default %q`, got, "me")
	}
	if provider.callCount != 1 {
		t.Errorf("provider was called %d times, want exactly 1", provider.callCount)
	}
}

// TestExecute_ASchemaDefaultFillsAMissingPathTokenInTheToolsPath is AC5: a
// default must be merged before the path is rendered, so it can fill a path
// token exactly like an explicitly-supplied argument would.
func TestExecute_ASchemaDefaultFillsAMissingPathTokenInTheToolsPath(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	tool := toolWithUserIdDefault(func(tool *catalog.ProviderTool) {
		tool.Path = "https://graph.microsoft.com/v1.0/users/{input.userId}"
	})
	f := execution.NewFacade(fakeToolReaderWithTool(tool), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://graph.microsoft.com/v1.0/users/me"
	if provider.lastReq.URL != want {
		t.Errorf("URL = %q, want %q (the userId default filling the path token)", provider.lastReq.URL, want)
	}
}

// TestExecute_ANestedObjectPropertysDefaultIsNotFilledOnlyTopLevelDefaultsApply
// is AC6: a nested property's own "default" (declared inside a top-level
// object property that itself has no default) must never be filled in — the
// top-level "profile" key stays absent, so a mapping entry referencing it is
// dropped entirely, not populated with a synthesized nested object.
func TestExecute_ANestedObjectPropertysDefaultIsNotFilledOnlyTopLevelDefaultsApply(t *testing.T) {
	tool := testTool()
	properties := tool.InputSchema["properties"].(map[string]any)
	properties["profile"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{"type": "string", "default": "guest"},
		},
	}
	tool.Mapping = catalog.Mapping{Body: map[string]string{"profileRole": "{input.profile}"}}
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(fakeToolReaderWithTool(tool), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(provider.lastReq.Body), &body); err != nil {
		t.Fatalf("could not decode provider request body %q as JSON: %v", provider.lastReq.Body, err)
	}
	if _, present := body["profileRole"]; present {
		t.Errorf(`body["profileRole"] = %v, want it entirely absent — a nested property's default must never synthesize the top-level "profile" argument`, body["profileRole"])
	}
}

// TestExecute_AToolWithNoSchemaDefaultsInventsNoArgument is AC7: a tool whose
// inputSchema declares no defaults at all builds the exact same request as
// before this slice existed — no argument is invented out of thin air.
func TestExecute_AToolWithNoSchemaDefaultsInventsNoArgument(t *testing.T) {
	provider := &fakeProviderClient{response: messagesResponse()}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(provider.lastReq.Query) != 0 {
		t.Errorf("Query = %+v, want empty — a schema with no declared defaults must never invent an argument", provider.lastReq.Query)
	}
}
