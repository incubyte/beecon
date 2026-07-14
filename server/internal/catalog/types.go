// Package catalog owns the Provider and Integration entities: Provider is a
// registry-described connector (name, logo, OAuth endpoints, scopes, tool
// definitions) loaded from a local declarative file in Phase 1; Integration
// pairs a Provider with one installation's own OAuth client id/secret and is
// visible to every organization in the installation (PD7).
package catalog

import "time"

// ProviderTool describes one tool a Provider exposes. Tools are addressed by
// slug (PD8/ADR-0006). Path is relative to the owning ProviderDefinition's
// BaseURL when BaseURL is set (the finalized definition format, PD13); a
// definition built directly in Go with no BaseURL may still set Path to a
// full call URL (Phase 1's shape, preserved for existing tests) — the
// execution module joins BaseURL and Path itself (execution/template.go).
// OutputSchema is required by the finalized format (PD13) so both vendors'
// unreliable schemas stop being a problem for Beecon's consumers. Mapping is
// the tool's declared query/header/body/pagination/file-input mapping — the
// zero value means "no declared mapping", which execution treats as Phase
// 1's generic argument pass-through.
type ProviderTool struct {
	Slug         string
	Name         string
	Description  string
	Method       string
	Path         string
	InputSchema  map[string]any
	OutputSchema map[string]any
	Deprecated   bool
	Mapping      Mapping
}

// Token-endpoint credential styles a provider definition may declare (PD13,
// Slice 2): CredentialStyleFormBody carries the Integration's client
// id/secret in the token request's form body; CredentialStyleBasicAuth
// carries them in an HTTP Basic Authorization header instead (RFC 6749
// section 2.3.1). CredentialStyleFormBody is also the default applied when a
// definition omits oauth.credentialStyle entirely — matching what Phase 1's
// Outlook token exchange already sends (verified in
// connections/driven/oauthhttp/client.go), so re-expressing Outlook without
// the field changes nothing about its behavior.
const (
	CredentialStyleFormBody  = "formBody"
	CredentialStyleBasicAuth = "basicAuth"
)

// ExpectedParam is one pre-auth value a provider definition may declare that
// the end user must supply before OAuth can even start (PD13's
// expectedParams, Slice 3) — a subdomain, an API key, anything the provider
// needs that is not part of the OAuth grant itself. Name is the key
// {params.x} templating and the connect page's submitted values address it
// by; DisplayName and Description are shown on the connect page's
// param-collection form. Secret marks a value masked in that form's input
// (type="password", AC5) and, like an OAuth token, always vault-encrypted
// before storage — it never appears in an API response or a log entry
// (AC7).
type ExpectedParam struct {
	Name        string
	DisplayName string
	Description string
	Required    bool
	Secret      bool
}

// ExpectedParamsView is what GetExpectedParams returns (Slice 3's catalog
// API): the provider's expected pre-auth param fields, plus the provider's
// own name — a consumer addressing an Integration by id alone still needs to
// know which provider it names.
type ExpectedParamsView struct {
	ProviderName string
	Fields       []ExpectedParam
}

// UserInfoMapping names which field of a provider's user-info/token-metadata
// response (PD13's userInfo mapping) the OAuth callback reads into a
// Connection's captured account metadata (PD9): Outlook's GET /v1.0/me
// carries "mail"/"displayName"; Hubspot's token-metadata endpoint carries
// "user"/"hub_domain" (PD16). A provider whose response carries neither
// field leaves the corresponding value empty.
type UserInfoMapping struct {
	EmailField       string
	DisplayNameField string
}

// ProviderDefinition is the parsed, validated form of one provider's
// declarative definition file. UserInfoURL is optional: it names the
// endpoint the OAuth callback calls (bearer-authenticated) to capture
// account metadata after token exchange (PD9 — for Outlook, Microsoft
// Graph's GET /v1.0/me); a provider with no such endpoint leaves it empty.
// BaseURL is the finalized format's provider-level call URL prefix (PD13);
// it is empty for a Phase-1-shaped definition built directly in Go, in which
// case every ProviderTool.Path is treated as a full URL.
type ProviderDefinition struct {
	Slug            string
	Name            string
	Logo            string
	AuthScheme      string
	BaseURL         string
	AuthorizeURL    string
	TokenURL        string
	UserInfoURL     string
	Scopes          []string
	CredentialStyle string
	UserInfo        UserInfoMapping
	ExpectedParams  []ExpectedParam
	Tools           []ProviderTool
	Triggers        []TriggerDefinition
}

// TriggerPollMapping is a trigger definition's poll-ingestion mapping (PD28,
// PD13's finalized definition format v1): which HTTP call the poller issues
// each tick, and how it reads records and their payload back out of the
// response. Path and Query/Body values are {config.x}/{watermark} templated
// the same way a tool's Mapping is {input.x}/{params.x} templated
// (execution/template.go). Slice 1 only parses and validates this shape;
// evaluating it against a live provider call is execution/poll.go's job
// (Slice 4).
type TriggerPollMapping struct {
	Method              string
	Path                string
	Query               map[string]string
	Body                map[string]string
	RecordsPath         string
	RecordIDPath        string
	RecordTimestampPath string
	Payload             map[string]string
}

// TriggerDefinition is one trigger a ProviderDefinition declares (PD28,
// PD35): ConfigSchema validates a trigger instance's config at creation
// (triggers.Facade.Create, Slice 2); PayloadSchema is the shape every fired
// trigger.event's data.payload conforms to. Ingestion is always "poll" today
// — a definition declaring "push" fails boot (AC5) — the field exists so a
// future push value arrives without a format bump (PD28). PollIntervalSeconds
// defaults to 60 and is clamped to the platform minimum when a definition
// declares less (with a boot log line).
type TriggerDefinition struct {
	Slug                string
	Name                string
	Description         string
	ConfigSchema        map[string]any
	PayloadSchema       map[string]any
	Ingestion           string
	PollIntervalSeconds int
	Poll                TriggerPollMapping
}

// TriggerDefinitionSummary is one trigger definition as
// ListTriggerDefinitions/TriggerDefinitionDetail return it (Slice 1's catalog
// API): the trigger's own fields plus the identifying details of the
// ProviderDefinition that owns it (API Shape's nested provider
// {slug, name, logo}) — the same shape ToolSummary already carries for
// tools. PollIntervalSeconds (Slice 4) is not part of the public API's
// documented response shape, but carries the already-clamped interval
// (PD28) the triggers module schedules each poll tick against — the same
// convention TriggerDefinition.PollIntervalSeconds already established.
type TriggerDefinitionSummary struct {
	Slug                string
	Name                string
	Description         string
	ConfigSchema        map[string]any
	PayloadSchema       map[string]any
	Ingestion           string
	PollIntervalSeconds int
	ProviderSlug        string
	ProviderName        string
	ProviderLogo        string
}

// TriggerDefinitionPage is one cursor-paginated page of trigger definitions
// (PD15's platform-wide convention), sorted by slug (mirrors ToolPage):
// NextCursor is empty when this was the last page.
type TriggerDefinitionPage struct {
	Items      []TriggerDefinitionSummary
	NextCursor string
}

// IntegrationID is minted only by CreateIntegration.
type IntegrationID string

// Integration pairs a Provider with one installation's OAuth client
// credentials. It is installation-level, not org-scoped (PD7): every
// organization in the installation may initiate a connection through it.
// ClientSecretEncrypted reports whether ClientSecret is vault ciphertext
// (PD17): every Integration created by NewIntegration is, but a row
// persisted before this phase's migration/backfill may still carry it in
// plaintext until EncryptPlaintextClientSecrets re-seals it at boot.
type Integration struct {
	ID                    IntegrationID
	ProviderSlug          string
	ClientID              string
	ClientSecret          string
	ClientSecretEncrypted bool
	CreatedAt             time.Time
}

// IntegrationSummary is what an organization sees when listing integrations
// (AC6): the OAuth client secret never appears here or in any other API
// response after creation.
type IntegrationSummary struct {
	ID           IntegrationID
	ProviderSlug string
	ProviderName string
	Logo         string
	AuthScheme   string
}

// NewIntegration constructs an Integration from an already vault-encrypted
// client secret (PD17: the facade encrypts before calling this, the same way
// connections.Connection.Activate only ever receives ciphertext) — every
// Integration NewIntegration builds is therefore ClientSecretEncrypted.
// Validation that providerSlug names a loaded ProviderDefinition happens in
// the facade, which is the only place that holds the set of loaded
// definitions.
func NewIntegration(id IntegrationID, providerSlug, clientID, encryptedClientSecret string, now time.Time) Integration {
	return Integration{
		ID:                    id,
		ProviderSlug:          providerSlug,
		ClientID:              clientID,
		ClientSecret:          encryptedClientSecret,
		ClientSecretEncrypted: true,
		CreatedAt:             now,
	}
}

// ToolSummary is one tool as ListTools/ToolDetail return it (Slice 1's
// catalog API): the tool's own catalog fields plus the identifying details
// of the ProviderDefinition that owns it, since a consumer addressing tools
// by slug alone (PD8) still needs to know which provider a tool belongs to.
type ToolSummary struct {
	Slug         string
	Name         string
	Description  string
	InputSchema  map[string]any
	OutputSchema map[string]any
	Deprecated   bool
	ProviderSlug string
	ProviderName string
	ProviderLogo string
}

// ToolPage is one cursor-paginated page of tools (PD15's platform-wide
// convention), sorted by slug (ADR-0006): NextCursor is empty when this was
// the last page.
type ToolPage struct {
	Items      []ToolSummary
	NextCursor string
}
