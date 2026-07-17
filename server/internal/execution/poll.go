// poll.go is the poll engine PD28/PD34 need: evaluating a trigger
// definition's declarative poll mapping (parsed and validated in Slice 1)
// against one live provider call, entirely generically — no
// Outlook/Hubspot-specific Go code, exactly like a tool's mapping
// (facade.go's buildToolCallURL/buildToolQuery/buildToolBody). The triggers
// module (Slice 4) is FetchTriggerRecords' only caller, reached through its
// own consumer-defined RecordSource port and an app/wiring.go adapter
// (BOUNDARIES: execution has no dependency on triggers, and triggers has
// none on execution).
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

// pollWatermarkFormat is the one canonical timestamp shape {watermark} is
// ever rendered as, and every record's own timestamp is parsed as (PD34):
// applied uniformly across every provider by construction, so no
// provider-specific Go code is ever needed here — Outlook's OData filter and
// Hubspot's search-body filter both compare against the identical string
// shape.
const pollWatermarkFormat = time.RFC3339

// PollQuery is FetchTriggerRecords' input (Slice 4): everything needed to
// evaluate one trigger definition's poll mapping against a live provider
// call for one instance's current tick. Watermark's zero value is the
// baseline-poll sentinel (PD34): rendered as {watermark}, it is simply the
// earliest possible timestamp, so a provider's "newer than watermark" filter
// naturally returns everything that currently exists — exactly what a
// baseline poll needs to observe, before triggers.ApplyWatermark decides
// none of it fires.
type PollQuery struct {
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID connections.ConnectionID
	TriggerSlug  string
	Config       map[string]any
	Watermark    time.Time
}

// Record is one provider record FetchTriggerRecords extracted from a poll
// response: its id (RecordIDPath), timestamp (RecordTimestampPath, parsed as
// pollWatermarkFormat), and payload (the poll mapping's own field map,
// evaluated against the raw record) — triggers.ApplyWatermark (Slice 4)
// decides which of a tick's Records are actually new.
type Record struct {
	ID        string
	Timestamp time.Time
	Payload   map[string]any
}

// PollResult is FetchTriggerRecords' output: every record the provider
// returned for this tick, unfiltered.
type PollResult struct {
	Records []Record
}

// FetchTriggerRecords evaluates trigger query.TriggerSlug's poll mapping
// against one live provider call (PD28/PD34): the mapping's request
// Path/Query/Body are {config.x}/{watermark} templated (template.go), the
// connection's access token is resolved through the existing
// ConnectionReader (inheriting PD18's reactive refresh-on-401, exactly like
// a tool execution's own callProvider), and PD21's rate-limit normalization
// and retry policy apply via the same retryLoop a tool execution uses. An
// unknown trigger slug is catalog's own not-found error.
func (f *Facade) FetchTriggerRecords(ctx context.Context, query PollQuery) (PollResult, error) {
	definition, trigger, err := f.triggerDefinitions.FindTriggerBySlug(ctx, query.TriggerSlug)
	if err != nil {
		return PollResult{}, err
	}

	access, err := f.connections.ResolveForExecution(ctx, query.OrgID, query.UserID, query.ConnectionID)
	if err != nil {
		return PollResult{}, err
	}
	if access.Status != connections.StatusActive {
		return PollResult{}, ErrValidation("connectionId", connectionNotActiveMessage(access.Status))
	}

	config := mergeWithSchemaDefaults(query.Config, trigger.ConfigSchema)
	request, err := buildPollRequest(definition.BaseURL, trigger.Poll, config, query.Watermark, access.AccessToken)
	if err != nil {
		return PollResult{}, err
	}

	response, err := f.fetchPollResponse(ctx, query, request)
	if err != nil {
		return PollResult{}, err
	}

	records, err := extractPollRecords(response.Body, trigger.Poll)
	if err != nil {
		return PollResult{}, err
	}
	return PollResult{Records: records}, nil
}

// fetchPollResponse makes the poll request, retrying against a normalized
// rate limit (PD21) and, on a 401, forcing exactly one on-demand token
// refresh and exactly one retried call — the same PD18 reactive path
// callProvider/retryAfterRefresh apply to tool execution.
func (f *Facade) fetchPollResponse(ctx context.Context, query PollQuery, request ToolCallRequest) (ToolCallResponse, error) {
	outcome := f.retryLoop(ctx, func() (ToolCallResponse, error) { return f.provider.Call(ctx, request) }, nil)
	if outcome.callErr == nil && outcome.response.StatusCode == http.StatusUnauthorized {
		return f.fetchPollResponseAfterRefresh(ctx, query, request)
	}
	return resolvePollOutcome(outcome)
}

func (f *Facade) fetchPollResponseAfterRefresh(ctx context.Context, query PollQuery, request ToolCallRequest) (ToolCallResponse, error) {
	access, err := f.connections.RefreshForExecution(ctx, query.OrgID, query.UserID, query.ConnectionID)
	if err != nil {
		return ToolCallResponse{}, err
	}
	if access.Status != connections.StatusActive {
		return ToolCallResponse{}, ErrValidation("connectionId", connectionNotActiveMessage(access.Status))
	}
	request.AccessToken = access.AccessToken
	outcome := f.retryLoop(ctx, func() (ToolCallResponse, error) { return f.provider.Call(ctx, request) }, nil)
	return resolvePollOutcome(outcome)
}

// resolvePollOutcome turns a retryOutcome into the poll response or an
// error: PD21's 429 carve-out when every attempt stayed rate-limited, the
// call error when the provider could not be reached at all, or a plain Go
// error naming the status for any other non-2xx response (a poll has no
// PD6 {successful,error} envelope to carry a tool-level failure in — the
// triggers module logs whatever error this surfaces and reschedules, PD34).
func resolvePollOutcome(outcome retryOutcome) (ToolCallResponse, error) {
	if outcome.exhausted {
		return ToolCallResponse{}, ErrRateLimited(outcome.retryAfter)
	}
	if outcome.callErr != nil {
		return ToolCallResponse{}, outcome.callErr
	}
	if outcome.response.StatusCode >= 400 {
		return ToolCallResponse{}, fmt.Errorf("poll request failed with status %d: %s", outcome.response.StatusCode, outcome.response.Body)
	}
	return outcome.response, nil
}

// buildPollRequest renders a trigger's poll mapping against config and
// watermark into a ToolCallRequest (reusing the exact same ProviderClient
// tool execution calls): the mapping's Path is {config.x} templated and
// joined onto the provider's BaseURL exactly like a tool's own Path
// (buildToolCallURL); Query and Body values may embed {config.x}/{watermark}
// anywhere inside a larger literal (RenderPollTemplate), unlike a tool's
// whole-token-only query/header mapping.
func buildPollRequest(baseURL string, poll catalog.TriggerPollMapping, config map[string]any, watermark time.Time, accessToken string) (ToolCallRequest, error) {
	watermarkStr := watermark.UTC().Format(pollWatermarkFormat)

	path, err := RenderPollTemplate(poll.Path, config, watermarkStr, true)
	if err != nil {
		return ToolCallRequest{}, err
	}
	query, err := renderPollValues(poll.Query, config, watermarkStr)
	if err != nil {
		return ToolCallRequest{}, err
	}
	body, err := buildPollBody(poll.Body, config, watermarkStr)
	if err != nil {
		return ToolCallRequest{}, err
	}

	return ToolCallRequest{
		Method:      poll.Method,
		URL:         joinBaseURLAndPath(baseURL, path),
		AccessToken: accessToken,
		Query:       query,
		Body:        body,
	}, nil
}

func renderPollValues(mapping map[string]string, config map[string]any, watermark string) (map[string]string, error) {
	if len(mapping) == 0 {
		return nil, nil
	}
	rendered := make(map[string]string, len(mapping))
	for param, expression := range mapping {
		value, err := RenderPollTemplate(expression, config, watermark, false)
		if err != nil {
			return nil, err
		}
		rendered[param] = value
	}
	return rendered, nil
}

// buildPollBody renders a poll mapping's Body (Hubspot's dotted
// filterGroups.* keys, PD35) into a JSON object: reuses setNestedBodyValue,
// the same dotted-key nesting a tool's own body mapping uses
// (buildToolBody).
func buildPollBody(mapping map[string]string, config map[string]any, watermark string) (string, error) {
	if len(mapping) == 0 {
		return "", nil
	}
	body := map[string]any{}
	for key, expression := range mapping {
		value, err := RenderPollTemplate(expression, config, watermark, false)
		if err != nil {
			return "", err
		}
		setNestedBodyValue(body, strings.Split(key, "."), value)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// extractPollRecords decodes a poll response body as JSON and reads its list
// of records back out at poll.RecordsPath (PD28/PD35: Outlook's "value",
// Hubspot's "results").
func extractPollRecords(body string, poll catalog.TriggerPollMapping) ([]Record, error) {
	var decoded any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return nil, fmt.Errorf("poll response was not valid JSON: %w", err)
	}
	rawRecords, ok := lookupPollField(decoded, poll.RecordsPath)
	if !ok {
		return nil, fmt.Errorf("poll response carried no records at %q", poll.RecordsPath)
	}
	list, ok := rawRecords.([]any)
	if !ok {
		return nil, fmt.Errorf("poll response's %q was not a list", poll.RecordsPath)
	}

	records := make([]Record, 0, len(list))
	for _, item := range list {
		record, err := pollRecordFrom(item, poll)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// pollRecordFrom extracts one Record's id, timestamp, and payload out of a
// raw decoded record, per the poll mapping's declared field paths.
func pollRecordFrom(item any, poll catalog.TriggerPollMapping) (Record, error) {
	id, ok := lookupPollField(item, poll.RecordIDPath)
	if !ok {
		return Record{}, fmt.Errorf("poll record carried no id at %q", poll.RecordIDPath)
	}
	timestampRaw, ok := lookupPollField(item, poll.RecordTimestampPath)
	if !ok {
		return Record{}, fmt.Errorf("poll record carried no timestamp at %q", poll.RecordTimestampPath)
	}
	timestampStr, ok := timestampRaw.(string)
	if !ok {
		return Record{}, fmt.Errorf("poll record's timestamp at %q was not a string", poll.RecordTimestampPath)
	}
	timestamp, err := time.Parse(pollWatermarkFormat, timestampStr)
	if err != nil {
		return Record{}, fmt.Errorf("poll record's timestamp %q did not parse as RFC3339: %w", timestampStr, err)
	}
	return Record{ID: fmt.Sprint(id), Timestamp: timestamp, Payload: buildPollPayload(item, poll.Payload)}, nil
}

// buildPollPayload evaluates a poll mapping's payload field map (PD35, e.g.
// Outlook's "from": "from.emailAddress.address") against one raw record: a
// field whose declared path is missing from the record is simply omitted
// from the payload.
func buildPollPayload(item any, mapping map[string]string) map[string]any {
	payload := make(map[string]any, len(mapping))
	for field, path := range mapping {
		if value, ok := lookupPollField(item, path); ok {
			payload[field] = value
		}
	}
	return payload
}

// lookupPollField walks a dotted field path (e.g. "from.emailAddress.address")
// through decoded JSON, returning the raw value found there, or false when
// any segment is missing or the wrong shape. An empty path returns data
// itself unchanged (a poll mapping whose RecordsPath is the response's own
// top-level array).
func lookupPollField(data any, path string) (any, bool) {
	if path == "" {
		return data, true
	}
	current := data
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
