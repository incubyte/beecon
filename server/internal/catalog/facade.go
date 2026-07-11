package catalog

import (
	"context"
	"time"
)

// Facade is the catalog module's only public surface.
type Facade struct {
	repo        Repository
	definitions map[string]ProviderDefinition
	newID       func() string
	now         func() time.Time
}

// NewFacade wires the facade with the installation's loaded provider
// definitions (AC1), an injected id minter, and a clock so tests can supply
// deterministic ids and a fixed time.
func NewFacade(repo Repository, definitions []ProviderDefinition, newID func() string, now func() time.Time) *Facade {
	byslug := make(map[string]ProviderDefinition, len(definitions))
	for _, d := range definitions {
		byslug[d.Slug] = d
	}
	return &Facade{repo: repo, definitions: byslug, newID: newID, now: now}
}

// CreateIntegration validates providerSlug against a loaded
// ProviderDefinition and persists a new Integration carrying the
// installation's OAuth client credentials. The returned summary never
// carries clientSecret (AC4: it appears in no API response after creation).
func (f *Facade) CreateIntegration(ctx context.Context, providerSlug, clientID, clientSecret string) (IntegrationSummary, error) {
	if _, ok := f.definitions[providerSlug]; !ok {
		return IntegrationSummary{}, ErrUnknownProvider(providerSlug)
	}
	integration := NewIntegration(IntegrationID(f.newID()), providerSlug, clientID, clientSecret, f.now())
	if err := f.repo.Save(ctx, integration); err != nil {
		return IntegrationSummary{}, err
	}
	return f.summarize(integration), nil
}

// GetIntegration fetches an Integration by id, translating a repository miss
// into ErrIntegrationNotFound. Integrations are installation-level (PD7), so
// this takes no organization id.
func (f *Facade) GetIntegration(ctx context.Context, id IntegrationID) (Integration, error) {
	integration, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Integration{}, err
	}
	if integration == nil {
		return Integration{}, ErrIntegrationNotFound()
	}
	return *integration, nil
}

// ListIntegrations returns every integration in the installation (PD7: every
// organization sees the same list in Phase 1), summarized with the provider
// name, logo, and auth scheme a consumer needs to start a connection.
func (f *Facade) ListIntegrations(ctx context.Context) ([]IntegrationSummary, error) {
	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]IntegrationSummary, 0, len(integrations))
	for _, integration := range integrations {
		summaries = append(summaries, f.summarize(integration))
	}
	return summaries, nil
}

// GetProviderDefinition returns the loaded ProviderDefinition for
// providerSlug: the connections module's OAuth handshake (Slice 4) needs the
// provider's authorize/token/user-info URLs and scopes to build the consent
// redirect and complete the token exchange. Translates an unknown slug into
// ErrUnknownProvider.
func (f *Facade) GetProviderDefinition(_ context.Context, providerSlug string) (ProviderDefinition, error) {
	definition, ok := f.definitions[providerSlug]
	if !ok {
		return ProviderDefinition{}, ErrUnknownProvider(providerSlug)
	}
	return definition, nil
}

func (f *Facade) summarize(integration Integration) IntegrationSummary {
	definition := f.definitions[integration.ProviderSlug]
	return IntegrationSummary{
		ID:           integration.ID,
		ProviderSlug: integration.ProviderSlug,
		ProviderName: definition.Name,
		Logo:         definition.Logo,
		AuthScheme:   definition.AuthScheme,
	}
}
