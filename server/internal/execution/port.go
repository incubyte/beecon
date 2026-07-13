package execution

import (
	"context"
	"io"

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
// encoded; empty for a tool with no body mapping (e.g. every GET tool). Files
// carries the tool's resolved file-typed inputs (PD22, Slice 7): a Call with
// a non-empty Files sends a multipart request instead of a JSON body — empty
// for every tool whose mapping declares no file-typed inputs.
type ToolCallRequest struct {
	Method      string
	URL         string
	AccessToken string
	Query       map[string]string
	Headers     map[string]string
	Body        string
	Files       []ToolCallFile
}

// ToolCallFile is one file-typed tool argument resolved to its stored bytes
// (PD22, Slice 7, AC4): FieldName is the tool input name the definition
// declared as file-typed (catalog.Mapping.FileInputs) and becomes the
// multipart form field's name. FileID and Size are carried alongside Content
// only so a log entry can record them without ever touching the bytes
// themselves (AC6). Content is read once already, in memory: files are
// capped at the facade's configured maximum size (AC3), and a
// ToolCallRequest is replayed verbatim across PD21's retry attempts, so a
// one-shot stream would send an empty body on any retry past the first.
type ToolCallFile struct {
	FieldName string
	FileID    string
	FileName  string
	MimeType  string
	Size      int64
	Content   []byte
}

// Files is the execution module's driven persistence port for uploaded file
// metadata (PD22, Slice 7): org-scoped the same way every other domain
// repository is, so FindByID enforces cross-organization not-found (AC2,
// AC5) without the facade having to check OrgID itself.
type Files interface {
	Save(ctx context.Context, file FileMetadata) error
	FindByID(ctx context.Context, org organizations.OrgID, id FileID) (*FileMetadata, error)
}

// FileStore is the execution module's driven byte-storage port (PD22): the
// Phase 2 adapter is local disk (driven/filestore/local.go); a real
// deployment moving off a single disk swaps in an S3/Azure-blob adapter
// behind this same port, unchanged (evolution trigger, no code built for it
// yet). StorageKey is FileMetadata's opaque handle — callers never construct
// one themselves.
type FileStore interface {
	Save(ctx context.Context, storageKey string, content io.Reader) error
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
	Delete(ctx context.Context, storageKey string) error
}

// ToolCallResponse is a provider's raw HTTP response to a tool call — the
// status code and body, before Execute turns a non-2xx status into a
// tool-level failure (AC7). RetryAfter carries the response's own
// Retry-After header verbatim (PD21) — empty when the provider sent none, in
// which case retry.go falls back to a jittered backoff.
type ToolCallResponse struct {
	StatusCode int
	Body       string
	RetryAfter string
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
// one tool call (AC8). RateLimited marks an attempt that IsRateLimited
// normalized as a rate limit (PD21, Slice 6) — every attempt writes its own
// entry, retried or not, so a rate-limited attempt that later succeeds still
// has its own marked entry alongside the successful one.
type LogEntry struct {
	OrgID        organizations.OrgID
	UserID       organizations.UserID
	ConnectionID connections.ConnectionID
	ToolSlug     string
	Status       int
	DurationMs   int64
	RequestBody  string
	ResponseBody string
	RateLimited  bool
}

// Recorder is a narrow, consumer-defined port for writing a tool-execution
// log entry (AC8), so tests can substitute a fake instead of depending on
// the logging module directly (BOUNDARIES: execution does not depend on
// logging — the composition root wires a logging-backed adapter).
type Recorder interface {
	Record(ctx context.Context, entry LogEntry) error
}
