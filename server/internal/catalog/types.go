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

// ProviderDefinition is the parsed, validated form of one provider's
// declarative definition file. UserInfoURL is optional: it names the
// endpoint the OAuth callback calls (bearer-authenticated) to capture
// account metadata after token exchange (PD9 — for Outlook, Microsoft
// Graph's GET /v1.0/me); a provider with no such endpoint leaves it empty.
// BaseURL is the finalized format's provider-level call URL prefix (PD13);
// it is empty for a Phase-1-shaped definition built directly in Go, in which
// case every ProviderTool.Path is treated as a full URL.
type ProviderDefinition struct {
	Slug         string
	Name         string
	Logo         string
	AuthScheme   string
	BaseURL      string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	Tools        []ProviderTool
}

// IntegrationID is minted only by CreateIntegration.
type IntegrationID string

// Integration pairs a Provider with one installation's OAuth client
// credentials. It is installation-level, not org-scoped (PD7): every
// organization in the installation may initiate a connection through it.
type Integration struct {
	ID           IntegrationID
	ProviderSlug string
	ClientID     string
	ClientSecret string
	CreatedAt    time.Time
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

// NewIntegration constructs an Integration. Validation that providerSlug
// names a loaded ProviderDefinition happens in the facade, which is the only
// place that holds the set of loaded definitions.
func NewIntegration(id IntegrationID, providerSlug, clientID, clientSecret string, now time.Time) Integration {
	return Integration{
		ID:           id,
		ProviderSlug: providerSlug,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		CreatedAt:    now,
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
