package execution

import (
	"context"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// ToolReader is a narrow, consumer-defined port satisfied by *catalog.Facade
// (BOUNDARIES: execution depends on catalog): Execute needs a tool's HTTP
// mapping (method, call URL) and input JSON Schema by slug alone (PD8: tools
// are addressed by slug in Phase 1). An unknown slug is not-found (AC3).
type ToolReader interface {
	FindToolBySlug(ctx context.Context, slug string) (catalog.ProviderDefinition, catalog.ProviderTool, error)
}

// ConnectionReader is a narrow, consumer-defined port satisfied by
// *connections.Facade (BOUNDARIES: execution depends on connections):
// Execute must confirm connectionID belongs to userID within org — unknown,
// cross-org, or cross-user is not-found (AC5, AC6) — before it ever calls
// the provider, and needs the connection's current status and (only when
// ACTIVE) its decrypted access token. Decryption happens inside connections;
// the vault stays private to that module. RefreshForExecution is PD18's
// reactive path (Slice 4): Execute calls it after a provider 401, forcing
// one refresh_token grant regardless of the connection's stored expiry, and
// retries the provider call exactly once with the result.
type ConnectionReader interface {
	ResolveForExecution(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id connections.ConnectionID) (connections.ExecutionAccess, error)
	RefreshForExecution(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id connections.ConnectionID) (connections.ExecutionAccess, error)
}

// ToolCallRequest is everything ProviderClient needs to make one HTTP call
// to a provider on the caller's behalf. Headers carries a tool's declared
// header mapping (PD13) — additional headers beyond the standard bearer
// Authorization/Accept every call already sends. Body is the tool's declared
// JSON body mapping (PD13, Hubspot's create-contact), already rendered and
// encoded; empty for a tool with no body mapping (e.g. every GET tool).
type ToolCallRequest struct {
	Method      string
	URL         string
	AccessToken string
	Query       map[string]string
	Headers     map[string]string
	Body        string
}

// ToolCallResponse is a provider's raw HTTP response to a tool call — the
// status code and body, before Execute turns a non-2xx status into a
// tool-level failure (AC7).
type ToolCallResponse struct {
	StatusCode int
	Body       string
}

// ProviderClient is a narrow driven port for calling a provider's tool
// endpoint, so tests can substitute a fake Graph server instead of calling
// the real internet. Call returns an error only when the provider could not
// be reached at all (e.g. a network failure) — a non-2xx HTTP response is a
// normal ToolCallResponse, not an error.
type ProviderClient interface {
	Call(ctx context.Context, req ToolCallRequest) (ToolCallResponse, error)
}

// LogEntry is what Execute hands to a Recorder after completing (or failing)
// one tool call (AC8).
type LogEntry struct {
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID connections.ConnectionID
	ToolSlug     string
	Status       int
	DurationMs   int64
	RequestBody  string
	ResponseBody string
}

// Recorder is a narrow, consumer-defined port for writing a tool-execution
// log entry (AC8), so tests can substitute a fake instead of depending on
// the logging module directly (BOUNDARIES: execution does not depend on
// logging — the composition root wires a logging-backed adapter).
type Recorder interface {
	Record(ctx context.Context, entry LogEntry) error
}
