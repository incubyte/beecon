package app

import (
	"context"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/delivery"
	"beecon/internal/execution"
	"beecon/internal/logging"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
)

// logRecorderAdapter is the shared shape behind every module's own
// Recorder port -> *logging.Facade adapter (Slice 3 tidy-first, FD11):
// connections.Recorder (Slice 5), execution.Recorder (Slice 6), and now
// delivery.Recorder (Slice 3 — the third consumer, exactly the Phase 2
// evolution trigger firing) all do the identical "wrap *logging.Facade,
// map this module's own LogEntry into logging.RecordInput, call Record"
// work. Generic over each module's own LogEntry type, so the mapping
// (toInput) is the only per-module code left; none of these modules
// depend on logging (BOUNDARIES) — only the composition root, which
// already depends on every module, may cross that boundary.
type logRecorderAdapter[T any] struct {
	logs    *logging.Facade
	toInput func(T) logging.RecordInput
}

func (a logRecorderAdapter[T]) Record(ctx context.Context, entry T) error {
	return a.logs.Record(ctx, a.toInput(entry))
}

// connectionsLogRecorder adapts *logging.Facade to connections.Recorder
// (Slice 5's retrofit of token-exchange logging onto Slice 4's oauth.go,
// AC8).
func connectionsLogRecorder(logs *logging.Facade) connections.Recorder {
	return logRecorderAdapter[connections.LogEntry]{logs: logs, toInput: func(entry connections.LogEntry) logging.RecordInput {
		return logging.RecordInput{
			OrgID:        entry.OrgID,
			UserID:       string(entry.UserID),
			ConnectionID: string(entry.ConnectionID),
			Kind:         logging.KindOAuthTokenExchange,
			Status:       entry.Status,
			DurationMs:   entry.DurationMs,
			RequestBody:  entry.RequestBody,
			ResponseBody: entry.ResponseBody,
		}
	}}
}

// executionLogRecorder adapts *logging.Facade to execution.Recorder
// (AC8).
func executionLogRecorder(logs *logging.Facade) execution.Recorder {
	return logRecorderAdapter[execution.LogEntry]{logs: logs, toInput: func(entry execution.LogEntry) logging.RecordInput {
		return logging.RecordInput{
			OrgID:        entry.OrgID,
			UserID:       string(entry.UserID),
			ConnectionID: string(entry.ConnectionID),
			ToolID:       entry.ToolID,
			ToolSlug:     entry.ToolSlug,
			Kind:         logging.KindToolExecution,
			Status:       entry.Status,
			DurationMs:   entry.DurationMs,
			RequestBody:  entry.RequestBody,
			ResponseBody: entry.ResponseBody,
			RateLimited:  entry.RateLimited,
		}
	}}
}

// deliveryLogRecorder adapts *logging.Facade to delivery.Recorder (Phase 3
// Slice 3): every delivery attempt writes one entry, always. EventType
// rides in ToolSlug — logging's own general-purpose "further classifier,
// where applicable" column — since Slice 3 adds no dedicated "event type"
// column, only EventID and Attempt (architecture section 6.10).
func deliveryLogRecorder(logs *logging.Facade) delivery.Recorder {
	return logRecorderAdapter[delivery.LogEntry]{logs: logs, toInput: func(entry delivery.LogEntry) logging.RecordInput {
		return logging.RecordInput{
			OrgID:      entry.OrgID,
			ToolSlug:   entry.EventType,
			Kind:       logging.KindWebhookDelivery,
			Status:     entry.Status,
			DurationMs: entry.DurationMs,
			EventID:    entry.EventID,
			Attempt:    entry.Attempt,
		}
	}}
}

// triggersLogRecorder adapts *logging.Facade to triggers.Recorder (Phase 3
// Slice 4, PD34): only a failing poll writes an entry — the error message
// rides in ResponseBody, mirroring how execution/delivery carry their own
// provider-response text there.
func triggersLogRecorder(logs *logging.Facade) triggers.Recorder {
	return logRecorderAdapter[triggers.LogEntry]{logs: logs, toInput: func(entry triggers.LogEntry) logging.RecordInput {
		return logging.RecordInput{
			OrgID:             entry.OrgID,
			UserID:            "",
			ConnectionID:      string(entry.ConnectionID),
			ToolSlug:          entry.TriggerSlug,
			Kind:              logging.KindTriggerPoll,
			ResponseBody:      entry.Error,
			TriggerInstanceID: string(entry.TriggerInstanceID),
		}
	}}
}

// executionRecordSource adapts *execution.Facade to triggers.RecordSource
// (Phase 3 Slice 4): triggers itself never imports execution (BOUNDARIES —
// the "Placement answers" seam), so PollOnce's fetch step reaches
// execution.FetchTriggerRecords only through this composition-root adapter.
type executionRecordSource struct {
	execution *execution.Facade
}

var _ triggers.RecordSource = executionRecordSource{}

func (a executionRecordSource) FetchRecords(ctx context.Context, query triggers.PollRecordQuery) ([]triggers.PollRecord, error) {
	result, err := a.execution.FetchTriggerRecords(ctx, execution.PollQuery{
		OrgID:        query.OrgID,
		UserID:       query.UserID,
		ConnectionID: query.ConnectionID,
		TriggerSlug:  query.TriggerSlug,
		Config:       query.Config,
		Watermark:    query.Watermark,
	})
	if err != nil {
		return nil, err
	}
	records := make([]triggers.PollRecord, 0, len(result.Records))
	for _, record := range result.Records {
		records = append(records, triggers.PollRecord{ID: record.ID, Timestamp: record.Timestamp, Payload: record.Payload})
	}
	return records, nil
}

// triggersEventSink adapts *delivery.Facade to triggers.EventSink (Phase 3
// Slice 4): triggers itself never imports delivery (BOUNDARIES — triggers
// and delivery talk only through this port), so a fired trigger.event
// reaches the outbox only through this composition-root adapter.
type triggersEventSink struct {
	delivery *delivery.Facade
}

var _ triggers.EventSink = triggersEventSink{}

func (a triggersEventSink) Enqueue(ctx context.Context, org organizations.OrgID, eventType string, data any) error {
	_, err := a.delivery.Enqueue(ctx, org, eventType, data)
	return err
}

// connectionsEventSink adapts *delivery.Facade to connections.EventSink
// (Phase 3 Slice 5, FD1): connections itself never imports delivery
// (BOUNDARIES) — every ACTIVE->EXPIRED transition reaches the outbox only
// through this composition-root adapter.
type connectionsEventSink struct {
	delivery *delivery.Facade
}

var _ connections.EventSink = connectionsEventSink{}

func (a connectionsEventSink) ConnectionExpired(ctx context.Context, org organizations.OrgID, data connections.ExpiredEventData) error {
	_, err := a.delivery.Enqueue(ctx, org, connections.EventTypeConnectionExpired, data)
	return err
}

// triggersDependents adapts *triggers.Facade to connections.Dependents
// (Phase 3, Slice 2, PD33): connections does not depend on triggers
// (BOUNDARIES — the dependency points the other way, triggers depends on
// connections) — only the composition root, which already depends on every
// module, may cross that boundary. Wired via connections.Facade's
// WithDependents so Delete's connection-delete cascade reaches
// triggers.Facade.DeleteByConnection without connections ever importing
// triggers.
type triggersDependents struct {
	triggers *triggers.Facade
}

var _ connections.Dependents = triggersDependents{}

func (a triggersDependents) OnConnectionDeleted(ctx context.Context, org organizations.OrgID, connID connections.ConnectionID) error {
	return a.triggers.DeleteByConnection(ctx, org, connID)
}

// catalogTriggerInstancePauser adapts *triggers.Facade to
// catalog.TriggerInstancePauser (Phase 5 registry sub-phase, Slice 4,
// PD66): catalog does not depend on triggers (BOUNDARIES — the dependency
// points the other way, triggers depends on catalog) — only the
// composition root, which already depends on every module, may cross that
// boundary. Wired via catalog.Facade's WithTriggerInstancePauser so
// Activate's removed-trigger safety net reaches
// triggers.Facade.PauseInstancesForRemovedTrigger without catalog ever
// importing triggers — mirrors triggersDependents' own adapter shape for
// the opposite module pairing.
type catalogTriggerInstancePauser struct {
	triggers *triggers.Facade
}

var _ catalog.TriggerInstancePauser = catalogTriggerInstancePauser{}

func (a catalogTriggerInstancePauser) PauseInstancesForRemovedTrigger(ctx context.Context, triggerSlug string) error {
	return a.triggers.PauseInstancesForRemovedTrigger(ctx, triggerSlug)
}
