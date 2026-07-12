// Package execution_test exercises execution.Facade.Execute against
// hand-written fakes for its four narrow ports (ToolReader, ConnectionReader,
// ProviderClient, Recorder) — Slice 5's AC1-AC8, with extra depth per the
// HIGH-risk callout: the provider must provably never be called on a
// platform-level or tool-level short-circuit, and the raw access token must
// never leak into a Result or its error message.
package execution_test

import (
	"context"
	"errors"
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

func TestExecute_AFailedUpstreamCallStillWritesALogEntryWithItsStatus(t *testing.T) {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 429, Body: "rate limited"}}
	recorder := &fakeRecorder{}
	f := execution.NewFacade(newFakeToolReader(), activeConnectionReader(), provider, recorder, fixedClock(time.Now()))

	_, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recorder.entries) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1", len(recorder.entries))
	}
	if recorder.entries[0].Status != 429 {
		t.Errorf("Status = %d, want %d", recorder.entries[0].Status, 429)
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
