package registryservice

import (
	"context"
	"fmt"
	"time"

	"beecon/internal/registrybundle"
)

// supportedFormatVersion is the only bundle formatVersion this registry
// accepts (ADR-0012: the registry serves exactly the same formatVersion: 1
// token-grammar format the embedded loader parses — no richer format).
const supportedFormatVersion = 1

// Facade is the registry service's only public surface (PD59): Publish
// mints tool_ ids, assigns/validates a version, runs the output-schema-vs-
// sample gate, and enforces the semver bump direction (Slice 2, PD62/PD63);
// Pull serves a specific version's bundle back to an authenticated
// installation.
type Facade struct {
	store     Store
	newToolID func() string
	now       func() time.Time
}

// NewFacade wires the facade with its driven Store, an injected tool_ id
// minter (idgen.Prefixed("tool_"), deterministic in tests), and a clock.
func NewFacade(store Store, newToolID func() string, now func() time.Time) *Facade {
	return &Facade{store: store, newToolID: newToolID, now: now}
}

// Publish stores bundle as providerSlug's next version, minting a fresh
// tool_ id for every tool slug this provider has never published before and
// reusing the same id for a slug it has already published (PD61: immutable,
// stable across versions). Slice 2 adds three publish-time gates ahead of
// storing anything: bundle.Tools must each declare an output schema and a
// sample the schema validates (PD63); the resolved version must be strictly
// greater than the provider's latest, with bump direction matching what
// tools/triggers were added or removed (PD62); and a version already on
// file is rejected as a conflict rather than silently overwritten. The
// caller may leave bundle.Version empty to have the registry compute the
// next version automatically (a removal bumps major, an addition bumps
// minor, anything else bumps patch) — the shape Slice 1's walking skeleton
// already exercised — or supply an explicit version, which the registry
// then enforces rather than assigns.
func (f *Facade) Publish(ctx context.Context, providerSlug string, bundle registrybundle.Bundle) (PublishResult, error) {
	if providerSlug == "" {
		return PublishResult{}, ErrValidation("providerSlug", "must not be empty")
	}
	if bundle.FormatVersion != supportedFormatVersion {
		return PublishResult{}, ErrValidation("formatVersion", "must be 1")
	}
	if err := validateOutputSchemasAgainstSamples(bundle.Tools); err != nil {
		return PublishResult{}, err
	}

	prior, err := f.priorBundle(ctx, providerSlug)
	if err != nil {
		return PublishResult{}, err
	}
	diff := diffBundle(prior, bundle)

	version, err := f.resolveVersion(ctx, providerSlug, bundle.Version, prior, diff)
	if err != nil {
		return PublishResult{}, err
	}

	finalized := bundle
	finalized.ProviderSlug = providerSlug
	finalized.Version = version
	finalized.Tools = mintToolIDs(bundle.Tools, priorToolIDsFrom(prior), f.newToolID)
	finalized.ContentHash, err = registrybundle.ContentHash(finalized)
	if err != nil {
		return PublishResult{}, err
	}

	if err := f.store.Save(ctx, providerSlug, StoredBundle{Bundle: finalized, PublishedAt: f.now()}); err != nil {
		return PublishResult{}, err
	}

	return PublishResult{
		Version:     version,
		ContentHash: finalized.ContentHash,
		Tools:       toolIdentities(finalized.Tools),
		Added:       BundleDiffItem{Tools: diff.addedTools, Triggers: diff.addedTriggers},
		Removed:     BundleDiffItem{Tools: diff.removedTools, Triggers: diff.removedTriggers},
	}, nil
}

// Pull returns providerSlug's bundle at version (Slice 1's pull API): a
// not-found error when the registry has never published that version.
func (f *Facade) Pull(ctx context.Context, providerSlug, version string) (registrybundle.Bundle, error) {
	stored, err := f.store.Find(ctx, providerSlug, version)
	if err != nil {
		return registrybundle.Bundle{}, err
	}
	if stored == nil {
		return registrybundle.Bundle{}, ErrBundleVersionNotFound(providerSlug, version)
	}
	return stored.Bundle, nil
}

// ListVersions returns every version providerSlug has published (Slice 3's
// review-before-adopting flow), each with its content hash and publish
// timestamp so an installation operator can see what's available before
// pulling or activating any of them. A provider that has never published
// returns an empty slice, not an error.
func (f *Facade) ListVersions(ctx context.Context, providerSlug string) ([]BundleVersionSummary, error) {
	stored, err := f.store.ListVersions(ctx, providerSlug)
	if err != nil {
		return nil, err
	}
	summaries := make([]BundleVersionSummary, 0, len(stored))
	for _, s := range stored {
		summaries = append(summaries, BundleVersionSummary{
			Version:     s.Bundle.Version,
			ContentHash: s.Bundle.ContentHash,
			PublishedAt: s.PublishedAt,
		})
	}
	return summaries, nil
}

// priorBundle returns providerSlug's latest published version's bundle, or
// nil for a provider that has never published before.
func (f *Facade) priorBundle(ctx context.Context, providerSlug string) (*registrybundle.Bundle, error) {
	latest, exists, err := f.store.LatestVersion(ctx, providerSlug)
	if err != nil || !exists {
		return nil, err
	}
	stored, err := f.store.Find(ctx, providerSlug, latest)
	if err != nil || stored == nil {
		return nil, err
	}
	return &stored.Bundle, nil
}

// priorToolIDsFrom returns prior's slug -> tool_ id map (PD61's stability
// requirement), or nil when prior is nil (a provider's first publish).
func priorToolIDsFrom(prior *registrybundle.Bundle) map[string]string {
	if prior == nil {
		return nil
	}
	ids := make(map[string]string, len(prior.Tools))
	for _, tool := range prior.Tools {
		ids[tool.Slug] = tool.ID
	}
	return ids
}

// resolveVersion assigns "1.0.0" to a provider's first bundle (Slice 1 AC);
// for every publish after that, it either auto-computes the next version by
// bump direction (requestedVersion == "") or validates a caller-supplied one
// against PD62's gates: not already published (version-conflict), strictly
// greater than the latest, and bumped in the direction diff requires.
func (f *Facade) resolveVersion(ctx context.Context, providerSlug, requestedVersion string, prior *registrybundle.Bundle, diff bundleDiff) (string, error) {
	if prior == nil {
		return resolveFirstVersion(requestedVersion)
	}
	priorVersion, err := parseSemver(prior.Version)
	if err != nil {
		return "", err
	}
	if requestedVersion == "" {
		return autoNextVersion(priorVersion, diff).String(), nil
	}
	return f.resolveRequestedVersion(ctx, providerSlug, requestedVersion, priorVersion, diff)
}

// resolveFirstVersion handles a provider's very first publish: "1.0.0" when
// the caller states no preference, otherwise the caller's own valid semver.
func resolveFirstVersion(requestedVersion string) (string, error) {
	if requestedVersion == "" {
		return "1.0.0", nil
	}
	if _, err := parseSemver(requestedVersion); err != nil {
		return "", ErrValidation("version", err.Error())
	}
	return requestedVersion, nil
}

// resolveRequestedVersion validates a caller-supplied version for a
// provider that has published before: it must not already be on file
// (version-conflict), must be strictly greater than priorVersion, and its
// bump must match what diff requires (removal -> major, addition -> at
// least minor).
func (f *Facade) resolveRequestedVersion(ctx context.Context, providerSlug, requestedVersion string, priorVersion semverParts, diff bundleDiff) (string, error) {
	existing, err := f.store.Find(ctx, providerSlug, requestedVersion)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", ErrVersionConflict(providerSlug, requestedVersion)
	}
	newVersion, err := parseSemver(requestedVersion)
	if err != nil {
		return "", ErrValidation("version", err.Error())
	}
	if newVersion.compare(priorVersion) <= 0 {
		return "", ErrIllegalSemverBump(providerSlug, fmt.Sprintf(
			"version %s must be strictly greater than the current latest version %s", requestedVersion, priorVersion))
	}
	if diff.hasRemovals() && newVersion.major <= priorVersion.major {
		return "", ErrIllegalSemverBump(providerSlug, "removing a tool or trigger requires a major version bump")
	}
	if diff.hasAdditions() && newVersion.major == priorVersion.major && newVersion.minor <= priorVersion.minor {
		return "", ErrIllegalSemverBump(providerSlug, "adding a tool or trigger requires at least a minor version bump")
	}
	return requestedVersion, nil
}

// mintToolIDs assigns each tool its registry identity: prior[slug] when
// this provider has published that slug before, a freshly minted tool_ id
// otherwise.
func mintToolIDs(tools []registrybundle.Tool, prior map[string]string, newToolID func() string) []registrybundle.Tool {
	minted := make([]registrybundle.Tool, len(tools))
	for i, tool := range tools {
		if id, ok := prior[tool.Slug]; ok {
			tool.ID = id
		} else {
			tool.ID = newToolID()
		}
		minted[i] = tool
	}
	return minted
}

func toolIdentities(tools []registrybundle.Tool) []ToolIdentity {
	identities := make([]ToolIdentity, 0, len(tools))
	for _, tool := range tools {
		identities = append(identities, ToolIdentity{ID: tool.ID, Slug: tool.Slug})
	}
	return identities
}
