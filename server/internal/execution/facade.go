package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// Facade is the execution module's only public surface.
type Facade struct {
	tools       ToolReader
	connections ConnectionReader
	provider    ProviderClient
	recorder    Recorder
	now         func() time.Time
}

// NewFacade wires the facade with the narrow cross-module reader ports for
// tools and connections, the driven provider client, the narrow Recorder
// port (AC8 — nil is safe: a facade built without a recorder simply skips
// logging), and a clock so tests can supply deterministic durations.
func NewFacade(tools ToolReader, connectionReader ConnectionReader, provider ProviderClient, recorder Recorder, now func() time.Time) *Facade {
	return &Facade{tools: tools, connections: connectionReader, provider: provider, recorder: recorder, now: now}
}

// Execute runs one tool call end to end (AC1-AC7). An unknown tool slug, or
// a connectionId that is unknown, belongs to another organization, or does
// not belong to userID, surfaces as a *httpx.DomainError not-found (PD6,
// AC3, AC5, AC6) before the provider is ever called. Everything else —
// invalid arguments against the tool's input schema (AC2), a connection
// that is not yet ACTIVE (AC4), or an upstream provider error (AC7) — is a
// tool-level failure inside a successful Result (PD6).
func (f *Facade) Execute(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	arguments map[string]any,
) (Result, error) {
	_, tool, err := f.tools.FindToolBySlug(ctx, toolSlug)
	if err != nil {
		return Result{}, err
	}

	access, err := f.connections.ResolveForExecution(ctx, org, userID, connectionID)
	if err != nil {
		return Result{}, err
	}
	if access.Status != connections.StatusActive {
		return FailureResult(CodeConnectionNotActive, connectionNotActiveMessage(access.Status)), nil
	}

	if violation := validateArguments(tool.InputSchema, arguments); violation != nil {
		return FailureResult(CodeInvalidArguments, violation.Error()), nil
	}

	return f.callProvider(ctx, org, userID, connectionID, toolSlug, tool, access.AccessToken, arguments)
}

func connectionNotActiveMessage(status connections.Status) string {
	return fmt.Sprintf("connection is %s, not ACTIVE", status)
}

// callProvider makes the one HTTP call a tool execution needs, times it,
// records it (AC8), and turns the outcome into a Result: success (AC1), a
// network failure reaching the provider, or an upstream 4xx/5xx (AC7).
func (f *Facade) callProvider(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	tool catalog.ProviderTool,
	accessToken string,
	arguments map[string]any,
) (Result, error) {
	request := ToolCallRequest{
		Method:      tool.Method,
		URL:         tool.Path,
		AccessToken: accessToken,
		Query:       stringifyArguments(arguments),
	}

	started := f.now()
	response, callErr := f.provider.Call(ctx, request)
	duration := f.now().Sub(started)

	status, responseBody := providerOutcome(response, callErr)
	f.recordAttempt(ctx, org, userID, connectionID, toolSlug, request, status, duration, responseBody)

	if callErr != nil {
		return FailureResult(CodeProviderUnavailable, "the provider could not be reached"), nil
	}
	if response.StatusCode >= 400 {
		return FailureResult(CodeProviderError, providerErrorMessage(response.StatusCode, response.Body)), nil
	}
	return SuccessResult(decodeResponseData(response.Body)), nil
}

func providerOutcome(response ToolCallResponse, callErr error) (int, string) {
	if callErr != nil {
		return 0, callErr.Error()
	}
	return response.StatusCode, response.Body
}

// recordAttempt writes one log entry for a completed or failed tool call
// (AC8). A nil recorder is a silent no-op; a recorder error never fails the
// tool execution itself — logging is observability, not a precondition of
// the primary operation.
func (f *Facade) recordAttempt(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	request ToolCallRequest,
	status int,
	duration time.Duration,
	responseBody string,
) {
	if f.recorder == nil {
		return
	}
	_ = f.recorder.Record(ctx, LogEntry{
		OrgID:        org,
		UserID:       userID,
		ConnectionID: connectionID,
		ToolSlug:     toolSlug,
		Status:       status,
		DurationMs:   duration.Milliseconds(),
		RequestBody:  toolCallRequestLogBody(request),
		ResponseBody: responseBody,
	})
}

// stringifyArguments turns the tool's validated arguments into query-string
// values: the tool's input schema (top/skip/select/filter for
// outlook-list-messages, PD8) already constrains what may be present, so
// this stays a generic pass-through rather than hardcoding parameter names.
func stringifyArguments(arguments map[string]any) map[string]string {
	query := make(map[string]string, len(arguments))
	for key, value := range arguments {
		query[key] = fmt.Sprint(value)
	}
	return query
}

func providerErrorMessage(status int, body string) string {
	return fmt.Sprintf("provider returned status %d: %s", status, body)
}

// decodeResponseData parses body as JSON for Result.Data (AC1); a provider
// response that is not valid JSON is surfaced as its raw string rather than
// failing the whole call.
func decodeResponseData(body string) any {
	var data any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return body
	}
	return data
}

// toolCallRequestLogBody builds a JSON representation of the tool call's
// request for logging (AC8). It carries the bearer access token in
// cleartext under "headers.Authorization" — this stays in memory only; the
// logging module redacts every sensitive field before the entry is ever
// persisted (AC9).
func toolCallRequestLogBody(request ToolCallRequest) string {
	body, _ := json.Marshal(map[string]any{
		"method": request.Method,
		"url":    request.URL,
		"headers": map[string]string{
			"Authorization": "Bearer " + request.AccessToken,
		},
		"query": request.Query,
	})
	return string(body)
}
