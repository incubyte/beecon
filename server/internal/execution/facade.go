package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
	definition, tool, err := f.tools.FindToolBySlug(ctx, toolSlug)
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

	return f.callProvider(ctx, org, userID, connectionID, toolSlug, definition, tool, access.AccessToken, arguments, toAnyMap(access.Params))
}

// toAnyMap converts a connection's decrypted pre-auth param values (Slice 3)
// into the map[string]any shape RenderPath/RenderMappedValue's params bag
// expects. Returns nil for a connection with no collected params, so a tool
// mapping with no {params.x} tokens behaves exactly as it did before params
// existed.
func toAnyMap(values map[string]string) map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func connectionNotActiveMessage(status connections.Status) string {
	return fmt.Sprintf("connection is %s, not ACTIVE", status)
}

// callProvider makes the HTTP call a tool execution needs, times it, records
// it (AC8), and turns the outcome into a Result: success (AC1), a network
// failure reaching the provider, or an upstream 4xx/5xx (AC7). A 401
// triggers PD18's reactive refresh path (Slice 4): exactly one on-demand
// token refresh, then exactly one retried call — never a second refresh,
// whatever that retried call itself returns.
func (f *Facade) callProvider(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	definition catalog.ProviderDefinition,
	tool catalog.ProviderTool,
	accessToken string,
	arguments map[string]any,
	params map[string]any,
) (Result, error) {
	requestURL, err := buildToolCallURL(definition.BaseURL, tool.Path, arguments, params)
	if err != nil {
		return FailureResult(CodeInvalidArguments, err.Error()), nil
	}
	requestBody, err := buildToolBody(tool.Mapping, arguments, params)
	if err != nil {
		return FailureResult(CodeInvalidArguments, err.Error()), nil
	}

	request := ToolCallRequest{
		Method:      tool.Method,
		URL:         requestURL,
		AccessToken: accessToken,
		Query:       buildToolQuery(tool.Mapping, arguments, params),
		Headers:     buildToolHeaders(tool.Mapping, arguments, params),
		Body:        requestBody,
	}

	response, callErr := f.attemptCall(ctx, org, userID, connectionID, toolSlug, request)
	if callErr == nil && response.StatusCode == http.StatusUnauthorized {
		return f.retryAfterRefresh(ctx, org, userID, connectionID, toolSlug, request, tool.Mapping.Pagination)
	}
	return toolCallResult(response, callErr, tool.Mapping.Pagination), nil
}

// attemptCall makes one HTTP call to the provider, times it, and records it
// (AC8) — shared by callProvider's first attempt and retryAfterRefresh's
// single retry, so every attempt (not just the first) writes its own log
// entry.
func (f *Facade) attemptCall(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	request ToolCallRequest,
) (ToolCallResponse, error) {
	started := f.now()
	response, callErr := f.provider.Call(ctx, request)
	duration := f.now().Sub(started)

	status, responseBody := providerOutcome(response, callErr)
	f.recordAttempt(ctx, org, userID, connectionID, toolSlug, request, status, duration, responseBody)
	return response, callErr
}

// retryAfterRefresh is PD18's reactive refresh path (Slice 4, AC7-AC9): a
// provider 401 triggers exactly one on-demand token refresh via the
// connections module, then exactly one retried call with the refreshed
// access token. A refresh that leaves the connection no longer ACTIVE (e.g.
// a revoked refresh token transitions it to EXPIRED, AC9) is reported as the
// same status-explaining tool-level failure a non-ACTIVE connection
// produces up front, without ever retrying the call.
func (f *Facade) retryAfterRefresh(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	request ToolCallRequest,
	pagination *catalog.Pagination,
) (Result, error) {
	access, err := f.connections.RefreshForExecution(ctx, org, userID, connectionID)
	if err != nil {
		return Result{}, err
	}
	if access.Status != connections.StatusActive {
		return FailureResult(CodeConnectionNotActive, connectionNotActiveMessage(access.Status)), nil
	}
	request.AccessToken = access.AccessToken
	response, callErr := f.attemptCall(ctx, org, userID, connectionID, toolSlug, request)
	return toolCallResult(response, callErr, pagination), nil
}

// toolCallResult turns one provider call's outcome into a Result: success
// (AC1), a network failure reaching the provider, or an upstream 4xx/5xx
// (AC7).
func toolCallResult(response ToolCallResponse, callErr error, pagination *catalog.Pagination) Result {
	if callErr != nil {
		return FailureResult(CodeProviderUnavailable, "the provider could not be reached")
	}
	if response.StatusCode >= 400 {
		return FailureResult(CodeProviderError, providerErrorMessage(response.StatusCode, response.Body))
	}
	data := decodeResponseData(response.Body)
	return SuccessResult(data, extractNextCursor(pagination, data))
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
// It is the fallback buildToolQuery uses for a tool that declares no
// Mapping.Query (Phase 1's shape, preserved for tests and definitions built
// directly in Go).
func stringifyArguments(arguments map[string]any) map[string]string {
	query := make(map[string]string, len(arguments))
	for key, value := range arguments {
		query[key] = fmt.Sprint(value)
	}
	return query
}

// buildToolCallURL renders {input.x}/{params.x} tokens in the tool's mapping
// path — params carries the connection's decrypted pre-auth param values
// (Slice 3, AC8), nil for a connection that collected none — then joins the
// result onto the provider's BaseURL. A tool built with no BaseURL (Phase 1's
// shape) treats its Path as the full call URL: RenderPath leaves a
// token-free path untouched, and joinBaseURLAndPath returns the path as-is
// when baseURL is empty.
func buildToolCallURL(baseURL, pathTemplate string, arguments, params map[string]any) (string, error) {
	renderedPath, err := RenderPath(pathTemplate, arguments, params)
	if err != nil {
		return "", err
	}
	return joinBaseURLAndPath(baseURL, renderedPath), nil
}

func joinBaseURLAndPath(baseURL, path string) string {
	if baseURL == "" {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

// buildToolQuery evaluates the tool's declared query mapping against
// arguments and params (Slice 3, AC8), then applies its declared
// pagination's canonical pageSize/cursor arguments (PD15b): a mapping entry
// whose input/param was not supplied is dropped (an optional argument the
// caller omitted). A tool with no declared query mapping, body mapping, or
// pagination at all falls back to stringifyArguments (Phase 1's generic
// pass-through) — a tool that declares only a body mapping (e.g.
// hubspot-create-contact) or only pagination (e.g. hubspot-list-contacts)
// does not.
func buildToolQuery(mapping catalog.Mapping, arguments, params map[string]any) map[string]string {
	if len(mapping.Query) == 0 && len(mapping.Body) == 0 && mapping.Pagination == nil {
		return stringifyArguments(arguments)
	}
	query := make(map[string]string, len(mapping.Query))
	for param, expression := range mapping.Query {
		if value, ok := RenderMappedValue(expression, arguments, params); ok {
			query[param] = value
		}
	}
	applyPaginationQuery(query, mapping.Pagination, arguments)
	return query
}

// buildToolHeaders evaluates the tool's declared header mapping against
// arguments and params the same way buildToolQuery does for query
// parameters. A tool with no declared header mapping sends no extra
// headers.
func buildToolHeaders(mapping catalog.Mapping, arguments, params map[string]any) map[string]string {
	if len(mapping.Header) == 0 {
		return nil
	}
	headers := make(map[string]string, len(mapping.Header))
	for name, expression := range mapping.Header {
		if value, ok := RenderMappedValue(expression, arguments, params); ok {
			headers[name] = value
		}
	}
	return headers
}

// buildToolBody evaluates the tool's declared JSON body mapping against
// arguments and params (PD13, Hubspot's create-contact; Slice 3's {params.x},
// AC8): a mapping entry whose input/param was not supplied is dropped, and a
// dotted key (e.g. "properties.email") builds a nested JSON object. A tool
// with no declared body mapping sends no request body at all.
func buildToolBody(mapping catalog.Mapping, arguments, params map[string]any) (string, error) {
	if len(mapping.Body) == 0 {
		return "", nil
	}
	body := map[string]any{}
	for key, expression := range mapping.Body {
		value, ok := RenderMappedValue(expression, arguments, params)
		if !ok {
			continue
		}
		setNestedBodyValue(body, strings.Split(key, "."), value)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// setNestedBodyValue sets value at path inside target, creating intermediate
// nested maps as needed (e.g. path ["properties","email"] builds
// {"properties":{"email":value}}).
func setNestedBodyValue(target map[string]any, path []string, value string) {
	if len(path) == 1 {
		target[path[0]] = value
		return
	}
	child, ok := target[path[0]].(map[string]any)
	if !ok {
		child = map[string]any{}
		target[path[0]] = child
	}
	setNestedBodyValue(child, path[1:], value)
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
		"body":  request.Body,
	})
	return string(body)
}
