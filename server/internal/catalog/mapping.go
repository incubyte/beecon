package catalog

// Mapping is a tool's declared HTTP mapping (PD13's finalized definition
// format v1): which provider query parameters and headers a tool call
// forwards from tool inputs, its JSON request-body shape, its pagination
// convention, and which inputs are file-typed. Every field is optional — a
// tool that declares no Mapping (Phase 1's shape, still constructible in Go
// for tests) falls back to the generic argument pass-through execution used
// before this format existed.
//
// Query and Header entries are declared as {providerName: expression}, where
// expression is a single {input.x}/{params.x} template token (e.g.
// "{input.select}") evaluated at call time (execution/template.go). Body
// mirrors Query's shape for a tool's JSON request body; wiring it into an
// actual HTTP body is a Slice 2 concern (Hubspot's create-contact) — Slice 1
// only needs the format to parse and validate it.
type Mapping struct {
	Query      map[string]string
	Header     map[string]string
	Body       map[string]string
	Pagination *Pagination
	FileInputs []string
}

// Pagination declares how a tool's provider pages a list response: the
// canonical pageSize/cursor inputs (PD15) map onto the provider's own
// parameter names, and NextCursorPath names the response field the
// following page's cursor is read from. Wiring this into an executed
// request/response is a Slice 2 concern (hubspot-list-contacts) — Slice 1
// only needs the format to parse and validate it.
type Pagination struct {
	PageSizeParam  string
	CursorParam    string
	NextCursorPath string
}
