// Package catalog owns the Provider and Integration entities: Provider is a
// registry-described connector (name, logo, OAuth endpoints, scopes, tool
// definitions) loaded from a local declarative file in Phase 1; Integration
// pairs a Provider with one installation's own OAuth client id/secret and is
// visible to every organization in the installation (PD7).
package catalog

import "time"

// ProviderTool describes one tool a Provider exposes. Phase 1 tools are
// addressed by slug (PD8). Path is the tool's full call URL (not a path
// relative to some other base) — the execution module calls it directly.
type ProviderTool struct {
	Slug        string
	Name        string
	Description string
	Method      string
	Path        string
	InputSchema map[string]any
}

// ProviderDefinition is the parsed, validated form of one provider's
// declarative definition file. UserInfoURL is optional: it names the
// endpoint the OAuth callback calls (bearer-authenticated) to capture
// account metadata after token exchange (PD9 — for Outlook, Microsoft
// Graph's GET /v1.0/me); a provider with no such endpoint leaves it empty.
type ProviderDefinition struct {
	Slug         string
	Name         string
	Logo         string
	AuthScheme   string
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
