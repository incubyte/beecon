package app

import (
	"context"

	"beecon/internal/connections"
	"beecon/internal/execution"
	"beecon/internal/logging"
)

// connectionsLogRecorder adapts *logging.Facade to connections.Recorder.
// connections does not depend on logging (BOUNDARIES) — only the
// composition root, which already depends on every module, may cross that
// boundary (Slice 5's retrofit of token-exchange logging onto Slice 4's
// oauth.go, AC8).
type connectionsLogRecorder struct {
	logs *logging.Facade
}

var _ connections.Recorder = connectionsLogRecorder{}

func (a connectionsLogRecorder) Record(ctx context.Context, entry connections.LogEntry) error {
	return a.logs.Record(ctx, logging.RecordInput{
		OrgID:        entry.OrgID,
		UserID:       string(entry.UserID),
		ConnectionID: string(entry.ConnectionID),
		Kind:         logging.KindOAuthTokenExchange,
		Status:       entry.Status,
		DurationMs:   entry.DurationMs,
		RequestBody:  entry.RequestBody,
		ResponseBody: entry.ResponseBody,
	})
}

// executionLogRecorder adapts *logging.Facade to execution.Recorder.
// execution does not depend on logging (BOUNDARIES) — the composition root
// wires this narrow adapter instead (AC8).
type executionLogRecorder struct {
	logs *logging.Facade
}

var _ execution.Recorder = executionLogRecorder{}

func (a executionLogRecorder) Record(ctx context.Context, entry execution.LogEntry) error {
	return a.logs.Record(ctx, logging.RecordInput{
		OrgID:        entry.OrgID,
		UserID:       string(entry.UserID),
		ConnectionID: string(entry.ConnectionID),
		ToolSlug:     entry.ToolSlug,
		Kind:         logging.KindToolExecution,
		Status:       entry.Status,
		DurationMs:   entry.DurationMs,
		RequestBody:  entry.RequestBody,
		ResponseBody: entry.ResponseBody,
		RateLimited:  entry.RateLimited,
	})
}
