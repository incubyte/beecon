package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// toolIDPrefix is the reserved tool_ id prefix (ADR-0003, PD61): a tool
// identifier carrying it is resolved by ProviderTool.ID rather than by
// slug — see FindToolBySlug.
const toolIDPrefix = "tool_"

// defaultPageLimit and maxPageLimit bound every in-memory list operation's
// page size (PD15) — tools and, since Slice 1, trigger definitions — when a
// caller supplies none, or supplies one larger than Beecon allows.
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// Facade is the catalog module's only public surface.
type Facade struct {
	repo Repository

	// definitionsMu guards definitions: Activate (Phase 5 registry
	// sub-phase, PD65) swaps a provider's served ProviderDefinition at
	// runtime, concurrently with every read below (ListTools,
	// FindToolBySlug, ...) — a plain map would race.
	definitionsMu sync.RWMutex
	definitions   map[string]ProviderDefinition

	newID      func() string
	now        func() time.Time
	vault      *vault.Vault
	governance organizations.GovernanceReader

	// registryClient and activatedDefinitions are Slice 1's optional
	// registry-sync add-on (PD64/PD65), wired via WithRegistrySync — nil
	// until then, exactly like execution.Facade's own WithFiles/
	// WithTriggerDefinitions convention. A facade with registryClient nil
	// (BEECON_REGISTRY_URL unset, PD59) can serve everything except
	// Activate, which fails clearly (ErrRegistryNotConfigured) rather than
	// panicking.
	registryClient       RegistryClient
	activatedDefinitions ActivatedDefinitions

	// newToolID mints tool_ ids for the boot backfill (Slice 6, PD68) —
	// BackfillEmbeddedSeed's own minter, distinct from newID (which only
	// ever mints intg_ ids): the registry mints tool_ ids for everything
	// that goes through publish/pull/activate, but an embedded provider
	// that has never been through the registry needs one minted locally,
	// once, at boot. Wired alongside activatedDefinitions by
	// WithRegistrySync since both are part of the same PD65 activated-
	// definition-store family; nil only in a facade built by a test that
	// never calls BackfillEmbeddedSeed.
	newToolID func() string

	// triggerInstancePauser is Slice 4's optional dependent-safety add-on
	// (PD66), wired via WithTriggerInstancePauser — nil until then, in which
	// case Activate simply never pauses any trigger-instance (the same
	// "optional add-on, no-op without it" convention every other With*
	// method on this facade already follows).
	triggerInstancePauser TriggerInstancePauser
}

// NewFacade wires the facade with the installation's loaded provider
// definitions (AC1), an injected id minter, a clock so tests can supply
// deterministic ids and a fixed time, the shared vault (PD17) every
// Integration client secret is encrypted under, and the governance reader
// (Slice 5, PD42/PD43) every integration/tool/trigger-definition listing
// filters through — catalog already depends on organizations, so this is
// referenced directly, no consumer-defined-port-plus-app-adapter needed.
func NewFacade(repo Repository, definitions []ProviderDefinition, newID func() string, now func() time.Time, tokenVault *vault.Vault, governance organizations.GovernanceReader) *Facade {
	byslug := make(map[string]ProviderDefinition, len(definitions))
	for _, d := range definitions {
		byslug[d.Slug] = d
	}
	return &Facade{repo: repo, definitions: byslug, newID: newID, now: now, vault: tokenVault, governance: governance}
}

// WithRegistrySync wires this facade's registry pull/activate support
// (PD64/PD65, Phase 5 registry sub-phase Slice 1): the driven RegistryClient
// port (an HTTP adapter in production, an in-memory fake in tests), the
// DB-backed ActivatedDefinitions store, and (Slice 6, PD68) the tool_ id
// minter BackfillEmbeddedSeed mints with. A facade built without this can
// still serve every embedded-seed operation unchanged; only Activate needs
// registryClient, only LoadActivatedDefinitions/BackfillEmbeddedSeed need
// activatedDefinitions, and only BackfillEmbeddedSeed needs newToolID — the
// same optional "With*" convention execution.Facade already uses for its own
// add-ons (WithFiles, WithTriggerDefinitions).
func (f *Facade) WithRegistrySync(client RegistryClient, activated ActivatedDefinitions, newToolID func() string) *Facade {
	f.registryClient = client
	f.activatedDefinitions = activated
	f.newToolID = newToolID
	return f
}

// WithTriggerInstancePauser wires this facade's dependent-trigger-instance
// safety net (PD66, Phase 5 registry sub-phase Slice 4): Activate calls it
// once per trigger slug a newly-activated version removes, so live
// trigger-instances bound to a disappearing trigger are paused rather than
// left polling a trigger definition that no longer exists. Optional — a
// facade built without one (WithRegistrySync's own tests, or any test that
// never exercises a removed trigger) simply never pauses anything.
func (f *Facade) WithTriggerInstancePauser(pauser TriggerInstancePauser) *Facade {
	f.triggerInstancePauser = pauser
	return f
}

// definitionsSnapshot returns a point-in-time copy of every loaded
// definition, safe to range over without holding definitionsMu — Activate
// replaces a definition wholesale (never mutates one in place), so a reader
// holding a snapshot always sees a fully-formed ProviderDefinition, old or
// new, never a half-updated one.
func (f *Facade) definitionsSnapshot() map[string]ProviderDefinition {
	f.definitionsMu.RLock()
	defer f.definitionsMu.RUnlock()
	snapshot := make(map[string]ProviderDefinition, len(f.definitions))
	for slug, definition := range f.definitions {
		snapshot[slug] = definition
	}
	return snapshot
}

// definitionByProviderSlug looks up one definition by its provider slug.
func (f *Facade) definitionByProviderSlug(slug string) (ProviderDefinition, bool) {
	f.definitionsMu.RLock()
	defer f.definitionsMu.RUnlock()
	definition, ok := f.definitions[slug]
	return definition, ok
}

// setDefinition installs definition as its provider slug's served
// definition — used by Activate (a runtime swap, Slice 1) and
// LoadActivatedDefinitions (the boot-time rebuild, PD65).
func (f *Facade) setDefinition(definition ProviderDefinition) {
	f.definitionsMu.Lock()
	defer f.definitionsMu.Unlock()
	f.definitions[definition.Slug] = definition
}

// deleteDefinition removes providerSlug's served definition entirely —
// Activate's rollback path (Slice 4, PD66) uses this only when the
// provider being activated had never been served at all before this call
// (hadPreviousDefinition false), so "roll back to the prior state" means
// "there was nothing being served," not "restore an earlier definition."
func (f *Facade) deleteDefinition(providerSlug string) {
	f.definitionsMu.Lock()
	defer f.definitionsMu.Unlock()
	delete(f.definitions, providerSlug)
}

// CreateIntegration validates providerSlug against a loaded
// ProviderDefinition and persists a new Integration carrying the
// installation's OAuth client credentials, encrypted under the vault before
// it is ever handed to the repository (PD17: only ciphertext is persisted).
// The returned summary never carries clientSecret (AC4: it appears in no API
// response after creation).
func (f *Facade) CreateIntegration(ctx context.Context, providerSlug, clientID, clientSecret string) (IntegrationSummary, error) {
	if _, ok := f.definitionByProviderSlug(providerSlug); !ok {
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

// GetVisibleIntegration returns id's Integration exactly as GetIntegration
// does, but additionally enforces org's governance (Slice 5's core-risk
// seam, PD42): an integration org cannot see — hidden, or omitted from a
// present allow-list — surfaces as ErrIntegrationNotFound, indistinguishable
// from the integration not existing at all. connections.Facade.Initiate
// calls this instead of GetIntegration so an org can never initiate a
// connection to an integration it cannot see (AC5).
func (f *Facade) GetVisibleIntegration(ctx context.Context, org organizations.OrgID, id IntegrationID) (Integration, error) {
	integration, err := f.GetIntegration(ctx, id)
	if err != nil {
		return Integration{}, err
	}
	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return Integration{}, err
	}
	if !governance.IsVisible(string(integration.ID)) {
		return Integration{}, ErrIntegrationNotFound()
	}
	return integration, nil
}

// EncryptPlaintextClientSecrets encrypts every Integration client secret
// still stored in plaintext (PD17: Phase 1 rows created before the vault
// existed) and persists the ciphertext, flipping ClientSecretEncrypted. It is
// idempotent — a row already marked ClientSecretEncrypted is left untouched
// — so it is safe to call once at every boot (app/wiring.go, after
// db.Migrate) regardless of how many times the installation has restarted.
// On success it logs how many rows it actually encrypted this run (PD38c,
// Phase 2 review carry-forward), including zero — so an operator can confirm
// the one-time migration ran (and, once it has fully caught up, confirm
// there was nothing left to do) rather than inferring it from silence.
func (f *Facade) EncryptPlaintextClientSecrets(ctx context.Context) error {
	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	encryptedCount := 0
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
		encryptedCount++
	}
	slog.Default().Info(fmt.Sprintf("encrypted %d plaintext client secrets", encryptedCount), "count", encryptedCount)
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
	definition, ok := f.definitionByProviderSlug(integration.ProviderSlug)
	if !ok {
		return ExpectedParamsView{}, ErrUnknownProvider(integration.ProviderSlug)
	}
	return ExpectedParamsView{ProviderName: definition.Name, Fields: definition.ExpectedParams}, nil
}

// ListIntegrations returns org's visible integrations (Slice 5, PD42):
// filtered by org's governance — allow-list (nil inherits the full
// installation catalog, exactly PD7's Phase 1 behavior) minus anything
// hidden — summarized with the provider name, logo, and auth scheme a
// consumer needs to start a connection.
func (f *Facade) ListIntegrations(ctx context.Context, org organizations.OrgID) ([]IntegrationSummary, error) {
	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return nil, err
	}
	summaries := make([]IntegrationSummary, 0, len(integrations))
	for _, integration := range integrations {
		if !governance.IsVisible(string(integration.ID)) {
			continue
		}
		summaries = append(summaries, f.summarize(integration))
	}
	return summaries, nil
}

// ListIntegrationsWithVisibility returns every installation integration,
// unfiltered, each annotated with its effective visibility for org (Slice 5,
// AC1) — the operator's governance view over the whole catalog, distinct
// from ListIntegrations' already-filtered, org-facing result.
func (f *Facade) ListIntegrationsWithVisibility(ctx context.Context, org organizations.OrgID) ([]IntegrationVisibility, error) {
	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return nil, err
	}
	items := make([]IntegrationVisibility, 0, len(integrations))
	for _, integration := range integrations {
		items = append(items, IntegrationVisibility{
			Integration: f.summarize(integration),
			Visibility:  effectiveVisibility(governance, string(integration.ID)),
		})
	}
	return items, nil
}

func effectiveVisibility(governance organizations.Governance, integrationID string) string {
	if governance.IsHidden(integrationID) {
		return VisibilityHidden
	}
	if governance.AllowList != nil && !containsAny(*governance.AllowList, integrationID) {
		return VisibilityNotAllowed
	}
	return VisibilityVisible
}

func containsAny(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ListFeaturedIntegrations returns org's onboarding "featured" integration
// subset (Slice 5, PD43), in the operator-configured order; when no featured
// list is configured, it falls back to the first FeaturedCap visible
// integrations (ListIntegrations' own order) so ?featured=true never
// surfaces an empty onboarding screen for an unconfigured org.
func (f *Facade) ListFeaturedIntegrations(ctx context.Context, org organizations.OrgID) ([]IntegrationSummary, error) {
	visible, err := f.ListIntegrations(ctx, org)
	if err != nil {
		return nil, err
	}
	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return nil, err
	}
	if len(governance.Featured) == 0 {
		return firstNIntegrations(visible, governance.FeaturedCap), nil
	}
	return orderByFeaturedList(visible, governance.Featured), nil
}

func firstNIntegrations(items []IntegrationSummary, n int) []IntegrationSummary {
	if n <= 0 || n >= len(items) {
		return items
	}
	return items[:n]
}

// orderByFeaturedList returns the subset of visible present in featured, in
// featured's own order — a featured id no longer visible (hidden, removed
// from the allow-list, or deleted) is silently skipped rather than
// resurfaced.
func orderByFeaturedList(visible []IntegrationSummary, featured []string) []IntegrationSummary {
	byID := make(map[IntegrationID]IntegrationSummary, len(visible))
	for _, summary := range visible {
		byID[summary.ID] = summary
	}
	ordered := make([]IntegrationSummary, 0, len(featured))
	for _, id := range featured {
		if summary, ok := byID[IntegrationID(id)]; ok {
			ordered = append(ordered, summary)
		}
	}
	return ordered
}

// GetProviderDefinition returns the loaded ProviderDefinition for
// providerSlug: the connections module's OAuth handshake (Slice 4) needs the
// provider's authorize/token/user-info URLs and scopes to build the consent
// redirect and complete the token exchange. Translates an unknown slug into
// ErrUnknownProvider.
func (f *Facade) GetProviderDefinition(_ context.Context, providerSlug string) (ProviderDefinition, error) {
	definition, ok := f.definitionByProviderSlug(providerSlug)
	if !ok {
		return ProviderDefinition{}, ErrUnknownProvider(providerSlug)
	}
	return definition, nil
}

// FindToolBySlug returns the ProviderDefinition and ProviderTool for a tool
// addressed by slug (PD8: tools are addressed by slug, across every loaded
// provider) or, since the Phase 5 registry sub-phase (PD61), by its
// immutable tool_ id — an identifier carrying the reserved tool_ prefix
// resolves against ProviderTool.ID instead, so execution.Facade.Execute
// (which calls this unchanged, via the ToolReader port) can be handed
// either a slug or a tool_ id at the same call site (ADR-0006's hand-off:
// additive, never a replacement — a genuine slug keeps resolving exactly as
// before). Translates an unknown identifier into ErrToolNotFound (AC3 of
// Slice 5; also the registry sub-phase's Slice 1 AC that an unknown tool_ id
// is a not-found, distinct from an execution failure).
func (f *Facade) FindToolBySlug(_ context.Context, idOrSlug string) (ProviderDefinition, ProviderTool, error) {
	byID := strings.HasPrefix(idOrSlug, toolIDPrefix)
	for _, definition := range f.definitionsSnapshot() {
		for _, tool := range definition.Tools {
			if toolMatchesIdentifier(tool, idOrSlug, byID) {
				return definition, tool, nil
			}
		}
	}
	return ProviderDefinition{}, ProviderTool{}, ErrToolNotFound()
}

func toolMatchesIdentifier(tool ProviderTool, idOrSlug string, byID bool) bool {
	if byID {
		return tool.ID == idOrSlug
	}
	return tool.Slug == idOrSlug
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

// ListTools returns every tool across every loaded ProviderDefinition that
// org may see (filtered by integration/provider, deprecation, and — Slice 5
// — org's governance), sorted by slug and cursor-paginated over that sort
// order (ADR-0006: tools are not database rows, so pagination happens in
// memory rather than through a repository query). The previously-ignored org
// param is now a real visibility query (PD42): a filter.IntegrationID this
// org cannot see returns an empty page rather than an error — the filter was
// valid, it just matched nothing this org may see — and, with no explicit
// filter, a provider whose every integration is hidden/non-allowed for org
// is dropped from the unfiltered list entirely.
func (f *Facade) ListTools(ctx context.Context, org organizations.OrgID, filter ToolFilter, cursor string, limit int) (ToolPage, error) {
	if filter.IntegrationID != "" {
		visible, err := f.integrationVisibleToOrg(ctx, org, filter.IntegrationID)
		if err != nil {
			return ToolPage{}, err
		}
		if !visible {
			return ToolPage{}, nil
		}
	}

	providerSlug, err := f.resolveToolFilterProviderSlug(ctx, filter)
	if err != nil {
		return ToolPage{}, err
	}

	visibleProviders, err := f.visibleProviderSlugs(ctx, org)
	if err != nil {
		return ToolPage{}, err
	}

	after, err := decodeSlugCursor(cursor)
	if err != nil {
		return ToolPage{}, err
	}

	tools := f.matchingToolSummaries(providerSlug, filter.IncludeDeprecated, visibleProviders)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Slug < tools[j].Slug })
	tools = toolsAfterCursor(tools, after)

	return paginateTools(tools, normalizePageLimit(limit)), nil
}

// integrationVisibleToOrg confirms id names a real integration (surfacing
// ErrIntegrationNotFound if not — the existing "unknown id" behavior is
// unchanged) and reports whether org's governance lets it see that
// integration.
func (f *Facade) integrationVisibleToOrg(ctx context.Context, org organizations.OrgID, id IntegrationID) (bool, error) {
	integration, err := f.GetIntegration(ctx, id)
	if err != nil {
		return false, err
	}
	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return false, err
	}
	return governance.IsVisible(string(integration.ID)), nil
}

// visibleProviderSlugs returns the set of provider slugs org may see (Slice
// 5, PD42): with no governance restriction (the continuity-preserving
// default — nil allow-list, nothing hidden) every loaded provider is
// visible, exactly Phase 1's behavior. Otherwise a provider slug stays
// visible when it has no integration at all (nothing concrete to hide,
// preserving ListTools' pre-governance "providerSlug filter works even with
// zero created integrations" behavior) or when at least one of its
// integrations remains visible to org; a provider whose every integration is
// hidden/non-allowed is dropped.
func (f *Facade) visibleProviderSlugs(ctx context.Context, org organizations.OrgID) (map[string]bool, error) {
	definitions := f.definitionsSnapshot()
	all := make(map[string]bool, len(definitions))
	for slug := range definitions {
		all[slug] = true
	}

	governance, err := f.governance.GetGovernance(ctx, org)
	if err != nil {
		return nil, err
	}
	if governance.AllowList == nil && len(governance.Hidden) == 0 {
		return all, nil
	}

	integrations, err := f.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	hasIntegration := make(map[string]bool)
	hasVisibleIntegration := make(map[string]bool)
	for _, integration := range integrations {
		hasIntegration[integration.ProviderSlug] = true
		if governance.IsVisible(string(integration.ID)) {
			hasVisibleIntegration[integration.ProviderSlug] = true
		}
	}

	visible := make(map[string]bool, len(all))
	for slug := range all {
		if !hasIntegration[slug] || hasVisibleIntegration[slug] {
			visible[slug] = true
		}
	}
	return visible, nil
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
	return f.resolveProviderSlugFilter(ctx, filter.IntegrationID, filter.ProviderSlug)
}

// resolveProviderSlugFilter turns an {integrationID, providerSlug} filter
// pair into the provider slug the caller should restrict its list to, or ""
// for no restriction — the shared resolution both ListTools and (Slice 1)
// ListTriggerDefinitions use. integrationID takes precedence when both are
// set; an unknown integration id or provider slug is not-found (integrations
// are installation-level, PD7 — there is no cross-org semantics to invent
// here, mirroring ListTools' own documented decision).
func (f *Facade) resolveProviderSlugFilter(ctx context.Context, integrationID IntegrationID, providerSlug string) (string, error) {
	if integrationID != "" {
		integration, err := f.GetIntegration(ctx, integrationID)
		if err != nil {
			return "", err
		}
		return integration.ProviderSlug, nil
	}
	if providerSlug != "" {
		if _, ok := f.definitionByProviderSlug(providerSlug); !ok {
			return "", ErrProviderNotFound()
		}
		return providerSlug, nil
	}
	return "", nil
}

func (f *Facade) matchingToolSummaries(providerSlugFilter string, includeDeprecated bool, visibleProviders map[string]bool) []ToolSummary {
	var summaries []ToolSummary
	for _, definition := range f.definitionsSnapshot() {
		if providerSlugFilter != "" && definition.Slug != providerSlugFilter {
			continue
		}
		if !visibleProviders[definition.Slug] {
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
		ID:           tool.ID,
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
		page.NextCursor = encodeSlugCursor(tools[len(tools)-1].Slug)
	}
	return page
}

func normalizePageLimit(requested int) int {
	if requested <= 0 {
		return defaultPageLimit
	}
	if requested > maxPageLimit {
		return maxPageLimit
	}
	return requested
}

func encodeSlugCursor(slug string) string {
	return httpx.EncodeCursor(slug)
}

func decodeSlugCursor(raw string) (string, error) {
	fields, err := httpx.DecodeCursor(raw, 1)
	if err != nil {
		return "", ErrInvalidCursor()
	}
	if fields == nil {
		return "", nil
	}
	return fields[0], nil
}

// TriggerDefinitionFilter narrows ListTriggerDefinitions' result set (Slice
// 1's catalog API): supply at most one of IntegrationID or ProviderSlug,
// resolved the same way ToolFilter is (resolveProviderSlugFilter).
type TriggerDefinitionFilter struct {
	IntegrationID IntegrationID
	ProviderSlug  string
}

// ListTriggerDefinitions returns every trigger across every loaded
// ProviderDefinition that org may see (filtered by integration/provider and
// — Slice 5 — org's governance), sorted by slug and cursor-paginated over
// that sort order — trigger definitions are not database rows, so pagination
// happens in memory the same way ListTools' does (ADR-0006). The
// previously-ignored org param is now a real visibility query, mirroring
// ListTools' own PD42 documented decision exactly.
func (f *Facade) ListTriggerDefinitions(ctx context.Context, org organizations.OrgID, filter TriggerDefinitionFilter, cursor string, limit int) (TriggerDefinitionPage, error) {
	if filter.IntegrationID != "" {
		visible, err := f.integrationVisibleToOrg(ctx, org, filter.IntegrationID)
		if err != nil {
			return TriggerDefinitionPage{}, err
		}
		if !visible {
			return TriggerDefinitionPage{}, nil
		}
	}

	providerSlug, err := f.resolveProviderSlugFilter(ctx, filter.IntegrationID, filter.ProviderSlug)
	if err != nil {
		return TriggerDefinitionPage{}, err
	}

	visibleProviders, err := f.visibleProviderSlugs(ctx, org)
	if err != nil {
		return TriggerDefinitionPage{}, err
	}

	after, err := decodeSlugCursor(cursor)
	if err != nil {
		return TriggerDefinitionPage{}, err
	}

	triggers := f.matchingTriggerDefinitionSummaries(providerSlug, visibleProviders)
	sort.Slice(triggers, func(i, j int) bool { return triggers[i].Slug < triggers[j].Slug })
	triggers = triggerDefinitionsAfterCursor(triggers, after)

	return paginateTriggerDefinitions(triggers, normalizePageLimit(limit)), nil
}

// TriggerDefinitionDetail returns one trigger definition by slug, across
// every loaded ProviderDefinition (mirrors ToolDetail/PD8's tools-by-slug
// convention, applied to triggers per PD14). An unknown slug is
// ErrTriggerDefinitionNotFound.
func (f *Facade) TriggerDefinitionDetail(_ context.Context, slug string) (TriggerDefinitionSummary, error) {
	for _, definition := range f.definitionsSnapshot() {
		for _, trigger := range definition.Triggers {
			if trigger.Slug == slug {
				return triggerDefinitionSummaryFrom(definition, trigger), nil
			}
		}
	}
	return TriggerDefinitionSummary{}, ErrTriggerDefinitionNotFound()
}

// FindTriggerBySlug returns the ProviderDefinition and full internal
// TriggerDefinition (poll mapping included) for a trigger addressed by slug,
// across every loaded provider (PD14, mirrors FindToolBySlug): the poll
// engine (execution/poll.go, Slice 4) needs the trigger's complete poll
// mapping and its owning provider's BaseURL, not just the public API's
// summarized shape (TriggerDefinitionDetail/TriggerDefinitionSummary).
// Translates an unknown slug into ErrTriggerDefinitionNotFound.
func (f *Facade) FindTriggerBySlug(_ context.Context, slug string) (ProviderDefinition, TriggerDefinition, error) {
	for _, definition := range f.definitionsSnapshot() {
		for _, trigger := range definition.Triggers {
			if trigger.Slug == slug {
				return definition, trigger, nil
			}
		}
	}
	return ProviderDefinition{}, TriggerDefinition{}, ErrTriggerDefinitionNotFound()
}

func (f *Facade) matchingTriggerDefinitionSummaries(providerSlugFilter string, visibleProviders map[string]bool) []TriggerDefinitionSummary {
	var summaries []TriggerDefinitionSummary
	for _, definition := range f.definitionsSnapshot() {
		if providerSlugFilter != "" && definition.Slug != providerSlugFilter {
			continue
		}
		if !visibleProviders[definition.Slug] {
			continue
		}
		for _, trigger := range definition.Triggers {
			summaries = append(summaries, triggerDefinitionSummaryFrom(definition, trigger))
		}
	}
	return summaries
}

func triggerDefinitionSummaryFrom(definition ProviderDefinition, trigger TriggerDefinition) TriggerDefinitionSummary {
	return TriggerDefinitionSummary{
		Slug:                trigger.Slug,
		Name:                trigger.Name,
		Description:         trigger.Description,
		ConfigSchema:        trigger.ConfigSchema,
		PayloadSchema:       trigger.PayloadSchema,
		Ingestion:           trigger.Ingestion,
		PollIntervalSeconds: trigger.PollIntervalSeconds,
		ProviderSlug:        definition.Slug,
		ProviderName:        definition.Name,
		ProviderLogo:        definition.Logo,
	}
}

// triggerDefinitionsAfterCursor returns the trigger definitions sorted
// strictly after the cursor's slug (ascending sort, so "after" means
// "greater than") — mirrors toolsAfterCursor.
func triggerDefinitionsAfterCursor(triggers []TriggerDefinitionSummary, after string) []TriggerDefinitionSummary {
	if after == "" {
		return triggers
	}
	idx := sort.Search(len(triggers), func(i int) bool { return triggers[i].Slug > after })
	return triggers[idx:]
}

func paginateTriggerDefinitions(triggers []TriggerDefinitionSummary, limit int) TriggerDefinitionPage {
	hasMore := len(triggers) > limit
	if hasMore {
		triggers = triggers[:limit]
	}
	page := TriggerDefinitionPage{Items: triggers}
	if hasMore {
		page.NextCursor = encodeSlugCursor(triggers[len(triggers)-1].Slug)
	}
	return page
}

func (f *Facade) summarize(integration Integration) IntegrationSummary {
	definition, _ := f.definitionByProviderSlug(integration.ProviderSlug)
	return IntegrationSummary{
		ID:           integration.ID,
		ProviderSlug: integration.ProviderSlug,
		ProviderName: definition.Name,
		Logo:         definition.Logo,
		AuthScheme:   definition.AuthScheme,
	}
}
