package catalog

import (
	"context"
	"sort"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// defaultToolPageLimit and maxToolPageLimit bound ListTools' page size
// (PD15) when a caller supplies none, or supplies one larger than Beecon
// allows.
const (
	defaultToolPageLimit = 50
	maxToolPageLimit     = 200
)

// Facade is the catalog module's only public surface.
type Facade struct {
	repo        Repository
	definitions map[string]ProviderDefinition
	newID       func() string
	now         func() time.Time
	vault       *vault.Vault
}

// NewFacade wires the facade with the installation's loaded provider
// definitions (AC1), an injected id minter, a clock so tests can supply
// deterministic ids and a fixed time, and the shared vault (PD17) every
// Integration client secret is encrypted under.
func NewFacade(repo Repository, definitions []ProviderDefinition, newID func() string, now func() time.Time, tokenVault *vault.Vault) *Facade {
	byslug := make(map[string]ProviderDefinition, len(definitions))
	for _, d := range definitions {
		byslug[d.Slug] = d
	}
	return &Facade{repo: repo, definitions: byslug, newID: newID, now: now, vault: tokenVault}
}

// CreateIntegration validates providerSlug against a loaded
// ProviderDefinition and persists a new Integration carrying the
// installation's OAuth client credentials, encrypted under the vault before
// it is ever handed to the repository (PD17: only ciphertext is persisted).
// The returned summary never carries clientSecret (AC4: it appears in no API
// response after creation).
func (f *Facade) CreateIntegration(ctx context.Context, providerSlug, clientID, clientSecret string) (IntegrationSummary, error) {
	if _, ok := f.definitions[providerSlug]; !ok {
		return IntegrationSummary{}, ErrUnknownProvider(providerSlug)
	}
	encryptedSecret, err := f.vault.Encrypt(clientSecret)
	if err != nil {
		return IntegrationSummary{}, err
	}
	integration := NewIntegration(IntegrationID(f.newID()), providerSlug, clientID, encryptedSecret, f.now())
	if err := f.repo.Save(ctx, integration); err != nil {
		return IntegrationSummary{}, err
	}
	return f.summarize(integration), nil
}

// GetIntegration fetches an Integration by id, translating a repository miss
// into ErrIntegrationNotFound, and decrypts its client secret before
// returning it (PD17) — every caller of this port (the connections module's
// OAuth handshake among them) keeps receiving the plaintext it always did;
// only the vault boundary moved. Integrations are installation-level (PD7),
// so this takes no organization id.
func (f *Facade) GetIntegration(ctx context.Context, id IntegrationID) (Integration, error) {
	integration, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Integration{}, err
	}
	if integration == nil {
		return Integration{}, ErrIntegrationNotFound()
	}
	return f.withDecryptedClientSecret(*integration)
}

// EncryptPlaintextClientSecrets encrypts every Integration client secret
// still stored in plaintext (PD17: Phase 1 rows created before the vault
// existed) and persists the ciphertext, flipping ClientSecretEncrypted. It is
// idempotent — a row already marked ClientSecretEncrypted is left untouched
// — so it is safe to call once at every boot (app/wiring.go, after
// db.Migrate) regardless of how many times the installation has restarted.
func (f *Facade) EncryptPlaintextClientSecrets(ctx context.Context) error {
	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, integration := range integrations {
		if integration.ClientSecretEncrypted {
			continue
		}
		encrypted, err := f.vault.Encrypt(integration.ClientSecret)
		if err != nil {
			return err
		}
		if err := f.repo.UpdateEncryptedClientSecret(ctx, integration.ID, encrypted); err != nil {
			return err
		}
	}
	return nil
}

// withDecryptedClientSecret returns a copy of integration with ClientSecret
// decrypted to plaintext when it is vault ciphertext. An integration not yet
// marked ClientSecretEncrypted (only possible for the brief window before
// EncryptPlaintextClientSecrets' boot backfill runs) is returned unchanged.
func (f *Facade) withDecryptedClientSecret(integration Integration) (Integration, error) {
	if !integration.ClientSecretEncrypted {
		return integration, nil
	}
	plaintext, err := f.vault.Decrypt(integration.ClientSecret)
	if err != nil {
		return Integration{}, err
	}
	integration.ClientSecret = plaintext
	return integration, nil
}

// GetExpectedParams returns id's provider's expected pre-auth params (Slice
// 3's catalog API): the fields the connect page must collect before OAuth
// can start, plus the provider's own name. An unknown integration id is
// ErrIntegrationNotFound; a loaded Integration whose provider slug names no
// loaded definition is ErrUnknownProvider (the same defensive case
// GetIntegration's callers already treat as fatal — definitions and
// Integrations are validated against each other at CreateIntegration).
func (f *Facade) GetExpectedParams(ctx context.Context, id IntegrationID) (ExpectedParamsView, error) {
	integration, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return ExpectedParamsView{}, err
	}
	if integration == nil {
		return ExpectedParamsView{}, ErrIntegrationNotFound()
	}
	definition, ok := f.definitions[integration.ProviderSlug]
	if !ok {
		return ExpectedParamsView{}, ErrUnknownProvider(integration.ProviderSlug)
	}
	return ExpectedParamsView{ProviderName: definition.Name, Fields: definition.ExpectedParams}, nil
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

// FindToolBySlug returns the ProviderDefinition and ProviderTool for a tool
// addressed by slug (PD8: tools are addressed by slug in Phase 1, across
// every loaded provider). Translates an unknown slug into ErrToolNotFound
// (AC3 of Slice 5).
func (f *Facade) FindToolBySlug(_ context.Context, slug string) (ProviderDefinition, ProviderTool, error) {
	for _, definition := range f.definitions {
		for _, tool := range definition.Tools {
			if tool.Slug == slug {
				return definition, tool, nil
			}
		}
	}
	return ProviderDefinition{}, ProviderTool{}, ErrToolNotFound()
}

// ToolFilter narrows ListTools' result set (Slice 1's catalog API): supply
// at most one of IntegrationID or ProviderSlug. IntegrationID resolves to
// its own Integration's ProviderSlug (an unknown integration id is
// not-found); ProviderSlug is matched directly against loaded definitions
// (an unknown provider slug is not-found). IncludeDeprecated defaults to
// excluding deprecated tools; set true to include them alongside their flag.
type ToolFilter struct {
	IntegrationID     IntegrationID
	ProviderSlug      string
	IncludeDeprecated bool
}

// ListTools returns every tool across every loaded ProviderDefinition
// (filtered by integration/provider and deprecation), sorted by slug and
// cursor-paginated over that sort order (ADR-0006: tools are not database
// rows, so pagination happens in memory rather than through a repository
// query). org is accepted for interface consistency with every other
// org-scoped list operation; Integrations remain installation-level (PD7),
// so it does not currently narrow the result.
func (f *Facade) ListTools(ctx context.Context, _ organizations.OrgID, filter ToolFilter, cursor string, limit int) (ToolPage, error) {
	providerSlug, err := f.resolveToolFilterProviderSlug(ctx, filter)
	if err != nil {
		return ToolPage{}, err
	}

	after, err := decodeToolCursor(cursor)
	if err != nil {
		return ToolPage{}, err
	}

	tools := f.matchingToolSummaries(providerSlug, filter.IncludeDeprecated)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Slug < tools[j].Slug })
	tools = toolsAfterCursor(tools, after)

	return paginateTools(tools, normalizeToolLimit(limit)), nil
}

// ToolDetail returns one tool by slug, across every loaded
// ProviderDefinition (PD8: tools are addressed by slug alone). An unknown
// slug is ErrToolNotFound.
func (f *Facade) ToolDetail(ctx context.Context, slug string) (ToolSummary, error) {
	definition, tool, err := f.FindToolBySlug(ctx, slug)
	if err != nil {
		return ToolSummary{}, err
	}
	return toolSummaryFrom(definition, tool), nil
}

// resolveToolFilterProviderSlug turns a ToolFilter into the provider slug
// ListTools should restrict to, or "" for no provider restriction.
// IntegrationID takes precedence when both are set.
func (f *Facade) resolveToolFilterProviderSlug(ctx context.Context, filter ToolFilter) (string, error) {
	if filter.IntegrationID != "" {
		integration, err := f.GetIntegration(ctx, filter.IntegrationID)
		if err != nil {
			return "", err
		}
		return integration.ProviderSlug, nil
	}
	if filter.ProviderSlug != "" {
		if _, ok := f.definitions[filter.ProviderSlug]; !ok {
			return "", ErrProviderNotFound()
		}
		return filter.ProviderSlug, nil
	}
	return "", nil
}

func (f *Facade) matchingToolSummaries(providerSlugFilter string, includeDeprecated bool) []ToolSummary {
	var summaries []ToolSummary
	for _, definition := range f.definitions {
		if providerSlugFilter != "" && definition.Slug != providerSlugFilter {
			continue
		}
		for _, tool := range definition.Tools {
			if tool.Deprecated && !includeDeprecated {
				continue
			}
			summaries = append(summaries, toolSummaryFrom(definition, tool))
		}
	}
	return summaries
}

func toolSummaryFrom(definition ProviderDefinition, tool ProviderTool) ToolSummary {
	return ToolSummary{
		Slug:         tool.Slug,
		Name:         tool.Name,
		Description:  tool.Description,
		InputSchema:  tool.InputSchema,
		OutputSchema: tool.OutputSchema,
		Deprecated:   tool.Deprecated,
		ProviderSlug: definition.Slug,
		ProviderName: definition.Name,
		ProviderLogo: definition.Logo,
	}
}

// toolsAfterCursor returns the tools sorted strictly after the cursor's slug
// (ascending sort, so "after" means "greater than").
func toolsAfterCursor(tools []ToolSummary, after string) []ToolSummary {
	if after == "" {
		return tools
	}
	idx := sort.Search(len(tools), func(i int) bool { return tools[i].Slug > after })
	return tools[idx:]
}

func paginateTools(tools []ToolSummary, limit int) ToolPage {
	hasMore := len(tools) > limit
	if hasMore {
		tools = tools[:limit]
	}
	page := ToolPage{Items: tools}
	if hasMore {
		page.NextCursor = encodeToolCursor(tools[len(tools)-1].Slug)
	}
	return page
}

func normalizeToolLimit(requested int) int {
	if requested <= 0 {
		return defaultToolPageLimit
	}
	if requested > maxToolPageLimit {
		return maxToolPageLimit
	}
	return requested
}

func encodeToolCursor(slug string) string {
	return httpx.EncodeCursor(slug)
}

func decodeToolCursor(raw string) (string, error) {
	fields, err := httpx.DecodeCursor(raw, 1)
	if err != nil {
		return "", ErrInvalidCursor()
	}
	if fields == nil {
		return "", nil
	}
	return fields[0], nil
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
