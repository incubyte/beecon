package catalog

import (
	"context"
	"encoding/base64"
	"sort"
	"time"

	"beecon/internal/organizations"
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
	return base64.RawURLEncoding.EncodeToString([]byte(slug))
}

func decodeToolCursor(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", ErrInvalidCursor()
	}
	return string(decoded), nil
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
