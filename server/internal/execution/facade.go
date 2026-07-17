package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/metrics"
	"beecon/internal/organizations"
)

// Facade is the execution module's only public surface.
type Facade struct {
	tools              ToolReader
	triggerDefinitions TriggerDefinitionReader
	connections        ConnectionReader
	provider           ProviderClient
	recorder           Recorder
	metrics            *metrics.Registry
	now                func() time.Time
	sleep              sleepFunc
	files              Files
	fileStore          FileStore
	maxFileBytes       int64
	newFileID          func() string
}

// NewFacade wires the facade with the narrow cross-module reader ports for
// tools and connections, the driven provider client, the narrow Recorder
// port (AC8 — nil is safe: a facade built without a recorder simply skips
// logging), and a clock so tests can supply deterministic durations. The
// retry loop's sleep (PD21) defaults to a real timer; see WithSleep.
func NewFacade(tools ToolReader, connectionReader ConnectionReader, provider ProviderClient, recorder Recorder, now func() time.Time) *Facade {
	return &Facade{tools: tools, connections: connectionReader, provider: provider, recorder: recorder, now: now, sleep: realSleep}
}

// WithSleep overrides the retry loop's sleep func (PD21). Production wiring
// never calls this — NewFacade already wires realSleep; tests use it to
// drive the retry loop deterministically, without a real sleep.
func (f *Facade) WithSleep(sleep func(ctx context.Context, d time.Duration) error) *Facade {
	f.sleep = sleep
	return f
}

// WithTriggerDefinitions wires this facade's FetchTriggerRecords support
// (Slice 4): the narrow TriggerDefinitionReader port (BOUNDARIES: execution
// depends on catalog) FetchTriggerRecords needs to resolve a trigger's poll
// mapping by slug. A facade built without this can still Execute tools —
// only FetchTriggerRecords needs it, the same "optional add-on wired via its
// own With*" convention as WithFiles.
func (f *Facade) WithTriggerDefinitions(reader TriggerDefinitionReader) *Facade {
	f.triggerDefinitions = reader
	return f
}

// WithMetrics wires this facade's Prometheus recording (PD24). A facade
// built without one (the nil zero value NewFacade leaves it at) makes every
// metrics call a silent no-op, exactly like a nil Recorder already does for
// logging.
func (f *Facade) WithMetrics(registry *metrics.Registry) *Facade {
	f.metrics = registry
	return f
}

// WithFiles wires this facade's FileUpload support (PD22, Slice 7): the
// Files metadata repository, the FileStore byte port, the configured
// maximum upload size (AC3), and the file id minter. Production wiring
// always calls this; a facade that never does can still Execute tools with
// no file-typed inputs — only UploadFile, DownloadFile, and a file-typed
// tool's execution need it.
func (f *Facade) WithFiles(files Files, fileStore FileStore, maxFileBytes int64, newFileID func() string) *Facade {
	f.files = files
	f.fileStore = fileStore
	f.maxFileBytes = maxFileBytes
	f.newFileID = newFileID
	return f
}

// UploadFile stores content behind the FileStore port and its metadata
// behind Files (PD22, AC1). Its true size is measured as it streams to
// storage (AC3): content is capped at maxFileBytes+1 bytes as it is read, so
// an oversized upload is rejected — and its partial bytes deleted — without
// ever buffering the whole file in memory first to check its length.
func (f *Facade) UploadFile(ctx context.Context, org organizations.OrgID, name, mimeType string, content io.Reader) (UploadedFile, error) {
	id := FileID(f.newFileID())
	storageKey := string(id)

	counted := &countingReader{reader: io.LimitReader(content, f.maxFileBytes+1)}
	if err := f.fileStore.Save(ctx, storageKey, counted); err != nil {
		return UploadedFile{}, err
	}
	if counted.count > f.maxFileBytes {
		_ = f.fileStore.Delete(ctx, storageKey)
		return UploadedFile{}, ErrValidation("file", fmt.Sprintf("exceeds the maximum allowed size of %d bytes", f.maxFileBytes))
	}

	metadata := FileMetadata{ID: id, OrgID: org, Name: name, MimeType: mimeType, Size: counted.count, StorageKey: storageKey, CreatedAt: f.now()}
	if err := f.files.Save(ctx, metadata); err != nil {
		return UploadedFile{}, err
	}
	return UploadedFile{ID: id, Name: name, MimeType: mimeType, Size: metadata.Size}, nil
}

// DownloadFile returns a file's metadata and content stream, org-scoped
// (AC2): an unknown or cross-organization id is not-found — no existence
// leak across organizations. The caller must close the returned stream.
func (f *Facade) DownloadFile(ctx context.Context, org organizations.OrgID, id FileID) (FileMetadata, io.ReadCloser, error) {
	metadata, err := f.files.FindByID(ctx, org, id)
	if err != nil {
		return FileMetadata{}, nil, err
	}
	if metadata == nil {
		return FileMetadata{}, nil, ErrFileNotFound()
	}
	content, err := f.fileStore.Open(ctx, metadata.StorageKey)
	if err != nil {
		return FileMetadata{}, nil, err
	}
	return *metadata, content, nil
}

// resolveFileInputs resolves every file-typed argument the tool's mapping
// declares (PD22) to its stored bytes, org-scoped, before the provider is
// ever called (AC5): an argument that is missing, not a string, or names a
// file_ id that does not exist or belongs to another organization is a
// tool-level failure — never an HTTP error, and never a provider call.
func (f *Facade) resolveFileInputs(ctx context.Context, org organizations.OrgID, fileInputs []string, arguments map[string]any) ([]ToolCallFile, *ExecutionError, error) {
	if len(fileInputs) == 0 {
		return nil, nil, nil
	}
	files := make([]ToolCallFile, 0, len(fileInputs))
	for _, name := range fileInputs {
		file, execErr, err := f.resolveOneFileInput(ctx, org, name, arguments)
		if err != nil || execErr != nil {
			return nil, execErr, err
		}
		files = append(files, file)
	}
	return files, nil, nil
}

// resolveOneFileInput resolves a single file-typed argument (AC5) and reads
// its stored bytes fully into memory: a file's size is already capped at
// upload time (AC3), and the resulting ToolCallFile must survive PD21's
// retry loop replaying the same ToolCallRequest more than once.
func (f *Facade) resolveOneFileInput(ctx context.Context, org organizations.OrgID, name string, arguments map[string]any) (ToolCallFile, *ExecutionError, error) {
	idValue, ok := arguments[name].(string)
	if !ok || idValue == "" {
		return ToolCallFile{}, fileNotFoundError(fmt.Sprintf("file input %q must be a previously uploaded file id", name)), nil
	}
	metadata, err := f.files.FindByID(ctx, org, FileID(idValue))
	if err != nil {
		return ToolCallFile{}, nil, err
	}
	if metadata == nil {
		return ToolCallFile{}, fileNotFoundError(fmt.Sprintf("file %q was not found", idValue)), nil
	}
	content, err := f.readFileContent(ctx, *metadata)
	if err != nil {
		return ToolCallFile{}, nil, err
	}
	return ToolCallFile{
		FieldName: name,
		FileID:    idValue,
		FileName:  metadata.Name,
		MimeType:  metadata.MimeType,
		Size:      metadata.Size,
		Content:   content,
	}, nil, nil
}

// readFileContent opens metadata's stored bytes and reads them fully,
// closing the stream before returning either way.
func (f *Facade) readFileContent(ctx context.Context, metadata FileMetadata) ([]byte, error) {
	stream, err := f.fileStore.Open(ctx, metadata.StorageKey)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	return io.ReadAll(stream)
}

func fileNotFoundError(message string) *ExecutionError {
	return &ExecutionError{Code: CodeFileNotFound, Message: message}
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

	arguments = mergeWithSchemaDefaults(arguments, tool.InputSchema)
	if violation := validateArguments(tool.InputSchema, arguments); violation != nil {
		return FailureResult(CodeInvalidArguments, violation.Error()), nil
	}

	return f.callProvider(ctx, org, userID, connectionID, toolSlug, definition, tool, access.AccessToken, arguments, toAnyMap(access.Params))
}

// mergeWithSchemaDefaults fills in every top-level property in schema that
// declares a "default" for a key values itself does not carry (PD35's
// folderId-defaults-to-Inbox behavior, and this slice's Gap C tool-input
// defaults, e.g. userId defaulting to "me"): a key already present in values
// — including an explicit null or empty string — is left untouched, since
// presence in the map, not truthiness of the value, is what "supplied" means.
// Only top-level properties are considered; a nested object property's own
// "default" is never applied. Shared by poll.go's trigger config merge and
// Execute's tool argument merge — both evaluate a JSON-Schema-shaped map the
// same way, against the same shape of schema.
func mergeWithSchemaDefaults(values map[string]any, valueSchema map[string]any) map[string]any {
	merged := make(map[string]any, len(values))
	for key, value := range values {
		merged[key] = value
	}
	properties, _ := valueSchema["properties"].(map[string]any)
	for key, raw := range properties {
		if _, exists := merged[key]; exists {
			continue
		}
		propertySchema, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if def, ok := propertySchema["default"]; ok {
			merged[key] = def
		}
	}
	return merged
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

// callProvider makes the HTTP call a tool execution needs, retrying it
// against a normalized upstream rate limit (PD21), times and records every
// attempt (AC8), and turns the final outcome into a Result: success (AC1), a
// network failure reaching the provider, or an upstream 4xx/5xx (AC7). A 401
// triggers PD18's reactive refresh path (Slice 4): exactly one on-demand
// token refresh, then exactly one retried call (itself retry-wrapped the
// same way) — never a second refresh, whatever that retried call itself
// returns. Retries exhausted while still rate-limited is PD21's ADR-0009
// carve-out (AC3): reported as a Go error, not a Result, so the HTTP handler
// renders it as 429 rather than the PD6 200 envelope.
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
	requestQuery, err := buildToolQuery(tool.Mapping, arguments, params)
	if err != nil {
		return FailureResult(CodeInvalidArguments, err.Error()), nil
	}
	requestHeaders, err := buildToolHeaders(tool.Mapping, arguments, params)
	if err != nil {
		return FailureResult(CodeInvalidArguments, err.Error()), nil
	}
	files, fileErr, err := f.resolveFileInputs(ctx, org, tool.Mapping.FileInputs, arguments)
	if err != nil {
		return Result{}, err
	}
	if fileErr != nil {
		return FailureResult(fileErr.Code, fileErr.Message), nil
	}

	request := ToolCallRequest{
		Method:      tool.Method,
		URL:         requestURL,
		AccessToken: accessToken,
		Query:       requestQuery,
		Headers:     requestHeaders,
		Body:        requestBody,
		Files:       files,
	}

	outcome := f.callWithRetry(ctx, org, userID, connectionID, toolSlug, definition.Slug, request)
	if outcome.exhausted {
		return Result{}, ErrRateLimited(outcome.retryAfter)
	}
	if outcome.callErr == nil && outcome.response.StatusCode == http.StatusUnauthorized {
		return f.retryAfterRefresh(ctx, org, userID, connectionID, toolSlug, definition.Slug, request, tool.Mapping.Pagination)
	}
	return toolCallResult(outcome.response, outcome.callErr, tool.Mapping.Pagination), nil
}

// attemptCall makes one HTTP call to the provider, times it, records it
// (AC8), and records its Prometheus outcome (PD24) — shared by every attempt
// callWithRetry's loop makes, so every attempt (not just the first) writes
// its own log entry and metric observation.
func (f *Facade) attemptCall(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	providerSlug string,
	request ToolCallRequest,
) (ToolCallResponse, error) {
	started := f.now()
	response, callErr := f.provider.Call(ctx, request)
	duration := f.now().Sub(started)

	status, responseBody := providerOutcome(response, callErr)
	rateLimited := callErr == nil && IsRateLimited(response)
	f.recordAttempt(ctx, org, userID, connectionID, toolSlug, request, status, duration, responseBody, rateLimited)
	f.recordExecutionMetric(providerSlug, status, duration)
	return response, callErr
}

func (f *Facade) recordExecutionMetric(providerSlug string, status int, duration time.Duration) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordToolExecution(providerSlug, status, duration)
}

// retryAfterRefresh is PD18's reactive refresh path (Slice 4, AC7-AC9): a
// provider 401 triggers exactly one on-demand token refresh via the
// connections module, then exactly one retry-wrapped call with the
// refreshed access token (PD21 applies to it too). A refresh that leaves the
// connection no longer ACTIVE (e.g. a revoked refresh token transitions it
// to EXPIRED, AC9) is reported as the same status-explaining tool-level
// failure a non-ACTIVE connection produces up front, without ever retrying
// the call.
func (f *Facade) retryAfterRefresh(
	ctx context.Context,
	org organizations.OrgID,
	userID organizations.UserID,
	connectionID connections.ConnectionID,
	toolSlug string,
	providerSlug string,
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
	outcome := f.callWithRetry(ctx, org, userID, connectionID, toolSlug, providerSlug, request)
	if outcome.exhausted {
		return Result{}, ErrRateLimited(outcome.retryAfter)
	}
	return toolCallResult(outcome.response, outcome.callErr, pagination), nil
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
// (AC8), marked RateLimited where IsRateLimited normalized this attempt as a
// rate limit (PD21). A nil recorder is a silent no-op; a recorder error
// never fails the tool execution itself — logging is observability, not a
// precondition of the primary operation.
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
	rateLimited bool,
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
		RateLimited:  rateLimited,
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
// pagination's canonical pageSize/cursor arguments (PD15b): a whole-token
// mapping entry whose input/param was not supplied is dropped (an optional
// argument the caller omitted); an embedded-token entry with a missing token
// is an error (Slice 1, Gap A) naming the missing token, and the provider is
// never called. A tool with no declared query mapping, body mapping, or
// pagination at all falls back to stringifyArguments (Phase 1's generic
// pass-through) — a tool that declares only a body mapping (e.g.
// hubspot-create-contact) or only pagination (e.g. hubspot-list-contacts)
// does not.
func buildToolQuery(mapping catalog.Mapping, arguments, params map[string]any) (map[string]string, error) {
	if len(mapping.Query) == 0 && len(mapping.Body) == 0 && mapping.Pagination == nil {
		return stringifyArguments(withoutFileInputs(arguments, mapping.FileInputs)), nil
	}
	query := make(map[string]string, len(mapping.Query))
	for param, expression := range mapping.Query {
		value, ok, err := RenderMappedValue(expression, arguments, params)
		if err != nil {
			return nil, err
		}
		if ok {
			query[param] = value
		}
	}
	applyPaginationQuery(query, mapping.Pagination, arguments)
	return query, nil
}

// withoutFileInputs drops every argument named in fileInputs (PD22): a
// file-typed argument names a stored file id, never a literal value the
// generic pass-through stringifyArguments falls back to for a tool with no
// other declared mapping (e.g. hubspot-upload-file) should ever leak into
// the provider's query string.
func withoutFileInputs(arguments map[string]any, fileInputs []string) map[string]any {
	if len(fileInputs) == 0 {
		return arguments
	}
	filtered := make(map[string]any, len(arguments))
	for key, value := range arguments {
		if !slices.Contains(fileInputs, key) {
			filtered[key] = value
		}
	}
	return filtered
}

// buildToolHeaders evaluates the tool's declared header mapping against
// arguments and params the same way buildToolQuery does for query
// parameters, including the whole-token-drop vs embedded-missing-error
// distinction. A tool with no declared header mapping sends no extra
// headers.
func buildToolHeaders(mapping catalog.Mapping, arguments, params map[string]any) (map[string]string, error) {
	if len(mapping.Header) == 0 {
		return nil, nil
	}
	headers := make(map[string]string, len(mapping.Header))
	for name, expression := range mapping.Header {
		value, ok, err := RenderMappedValue(expression, arguments, params)
		if err != nil {
			return nil, err
		}
		if ok {
			headers[name] = value
		}
	}
	return headers, nil
}

// buildToolBody evaluates the tool's declared JSON body mapping against
// arguments and params (PD13, Hubspot's create-contact; Slice 3's {params.x},
// AC8): a whole-token mapping entry whose input/param was not supplied is
// dropped, an embedded-token entry with a missing token is an error (Slice 1,
// Gap A), and a dotted key (e.g. "properties.email") builds a nested JSON
// object. A tool with no declared body mapping sends no request body at all.
func buildToolBody(mapping catalog.Mapping, arguments, params map[string]any) (string, error) {
	if len(mapping.Body) == 0 {
		return "", nil
	}
	body := map[string]any{}
	for key, expression := range mapping.Body {
		value, ok, err := RenderMappedValue(expression, arguments, params)
		if err != nil {
			return "", err
		}
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
		"files": fileLogEntries(request.Files),
	})
	return string(body)
}

// fileLogEntries builds the file id + size a log entry may safely record for
// a tool call's file-typed arguments (AC6): ToolCallFile.Content is never
// referenced here, so the actual bytes never reach a log entry.
func fileLogEntries(files []ToolCallFile) []map[string]any {
	if len(files) == 0 {
		return nil
	}
	entries := make([]map[string]any, 0, len(files))
	for _, file := range files {
		entries = append(entries, map[string]any{
			"fieldName": file.FieldName,
			"id":        file.FileID,
			"size":      file.Size,
		})
	}
	return entries
}
