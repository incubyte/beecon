// Package registrybundle is the wire format the separate registry service
// and an installation's catalog module exchange over the pull API (Phase 5
// registry sub-phase, PD59/PD64): the same formatVersion: 1 provider-
// definition shape the installation's embedded YAML loader already parses
// (catalog/definition_v1.go), transmitted as JSON instead of YAML, with each
// tool's registry-minted immutable tool_ id carried alongside its slug
// (PD61). It is shared infrastructure — plain data, no behavior — with no
// dependency on any domain module, so both the registry binary (cmd/registry)
// and the installation binary (cmd/beecon) can import it without violating
// BOUNDARIES.md's module graph.
package registrybundle

// Bundle is one provider's published definition at a specific version
// (PD62: one provider per bundle at a semver, plus a content hash for
// integrity). ProviderSlug, Version, and ContentHash are set by the
// registry at publish — a publish request carries them empty (Version/
// ContentHash) or advisory (ProviderSlug, which the registry's own path
// parameter is authoritative over).
type Bundle struct {
	FormatVersion  int             `json:"formatVersion"`
	ProviderSlug   string          `json:"providerSlug"`
	Version        string          `json:"version,omitempty"`
	ContentHash    string          `json:"contentHash,omitempty"`
	Name           string          `json:"name"`
	Logo           string          `json:"logo,omitempty"`
	AuthScheme     string          `json:"authScheme,omitempty"`
	BaseURL        string          `json:"baseUrl"`
	OAuth          OAuthConfig     `json:"oauth"`
	ExpectedParams []ExpectedParam `json:"expectedParams,omitempty"`
	Tools          []Tool          `json:"tools"`
	Triggers       []Trigger       `json:"triggers,omitempty"`
}

// OAuthConfig mirrors catalog's ProviderDefinition OAuth fields exactly, so
// converting a Bundle into a catalog.ProviderDefinition (and back) is a
// direct field-for-field mapping.
type OAuthConfig struct {
	AuthorizeURL             string   `json:"authorizeUrl"`
	TokenURL                 string   `json:"tokenUrl"`
	UserInfoURL              string   `json:"userInfoUrl,omitempty"`
	Scopes                   []string `json:"scopes"`
	CredentialStyle          string   `json:"credentialStyle,omitempty"`
	UserInfoEmailField       string   `json:"userInfoEmailField,omitempty"`
	UserInfoDisplayNameField string   `json:"userInfoDisplayNameField,omitempty"`
}

// ExpectedParam mirrors catalog.ExpectedParam.
type ExpectedParam struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

// Tool mirrors catalog.ProviderTool. ID is empty in a publish request — the
// registry mints it (PD61) and returns it filled in every subsequent pull.
// Sample is PD63's Slice 2 field (a recorded real response the registry
// validates OutputSchema against at publish); carried here so the wire
// shape is stable across slices, unused by Slice 1.
type Tool struct {
	ID           string         `json:"id,omitempty"`
	Slug         string         `json:"slug"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Deprecated   bool           `json:"deprecated,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema"`
	Sample       map[string]any `json:"sample,omitempty"`
	Mapping      ToolMapping    `json:"mapping"`
}

// ToolMapping mirrors catalog.Mapping plus the tool's method/path (which
// live directly on catalog.ProviderTool rather than catalog.Mapping).
type ToolMapping struct {
	Method     string            `json:"method"`
	Path       string            `json:"path"`
	Query      map[string]string `json:"query,omitempty"`
	Header     map[string]string `json:"header,omitempty"`
	Body       map[string]string `json:"body,omitempty"`
	Pagination *Pagination       `json:"pagination,omitempty"`
	FileInputs []string          `json:"fileInputs,omitempty"`
}

// Pagination mirrors catalog.Pagination.
type Pagination struct {
	PageSizeParam  string `json:"pageSizeParam,omitempty"`
	CursorParam    string `json:"cursorParam,omitempty"`
	NextCursorPath string `json:"nextCursorPath,omitempty"`
}

// Trigger mirrors catalog.TriggerDefinition.
type Trigger struct {
	Slug                string         `json:"slug"`
	Name                string         `json:"name"`
	Description         string         `json:"description,omitempty"`
	ConfigSchema        map[string]any `json:"configSchema"`
	PayloadSchema       map[string]any `json:"payloadSchema"`
	Ingestion           string         `json:"ingestion"`
	PollIntervalSeconds int            `json:"pollIntervalSeconds,omitempty"`
	Poll                TriggerPoll    `json:"poll"`
}

// TriggerPoll mirrors catalog.TriggerPollMapping.
type TriggerPoll struct {
	Method              string            `json:"method"`
	Path                string            `json:"path"`
	Query               map[string]string `json:"query,omitempty"`
	Body                map[string]string `json:"body,omitempty"`
	RecordsPath         string            `json:"recordsPath"`
	RecordIDPath        string            `json:"recordIdPath"`
	RecordTimestampPath string            `json:"recordTimestampPath"`
	Payload             map[string]string `json:"payload"`
}
