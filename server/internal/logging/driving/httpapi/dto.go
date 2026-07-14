package httpapi

import "beecon/internal/logging"

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"

// logEntryDTO is one entry in GET /api/v1/logs's response (AC10): ids, tool
// slug, HTTP status, duration, and the already-redacted request/response
// bodies (AC9).
type logEntryDTO struct {
	ID           string `json:"id"`
	OrgID        string `json:"organizationId"`
	UserID       string `json:"userId,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
	ToolSlug     string `json:"toolSlug,omitempty"`
	Kind         string `json:"kind"`
	Status       int    `json:"status"`
	DurationMs   int64  `json:"durationMs"`
	RequestBody  string `json:"requestBody"`
	ResponseBody string `json:"responseBody"`
	RateLimited  bool   `json:"rateLimited"`
	EventID      string `json:"eventId,omitempty"`
	Attempt      int    `json:"attempt,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

// logsPageDTO is one cursor-paginated page of log entries (PD10):
// nextCursor is absent when this was the last page.
type logsPageDTO struct {
	Entries    []logEntryDTO `json:"entries"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

func toLogEntryDTO(entry logging.EventLog) logEntryDTO {
	return logEntryDTO{
		ID:           string(entry.ID),
		OrgID:        string(entry.OrgID),
		UserID:       entry.UserID,
		ConnectionID: entry.ConnectionID,
		ToolSlug:     entry.ToolSlug,
		Kind:         string(entry.Kind),
		Status:       entry.Status,
		DurationMs:   entry.DurationMs,
		RequestBody:  entry.RequestBody,
		ResponseBody: entry.ResponseBody,
		RateLimited:  entry.RateLimited,
		EventID:      entry.EventID,
		Attempt:      entry.Attempt,
		CreatedAt:    entry.CreatedAt.Format(rfc3339Millis),
	}
}

func toLogsPageDTO(result logging.QueryResult) logsPageDTO {
	entries := make([]logEntryDTO, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, toLogEntryDTO(entry))
	}
	return logsPageDTO{Entries: entries, NextCursor: result.NextCursor}
}
