// Phase 5 registry sub-phase, Slice 6 (PD68): the boot-time half of the
// embedded-provider migration. The registry-side half is the one-time
// publish script (cmd/publishembeddedproviders) that loads the Outlook and
// Hubspot embedded YAML into the registry as their initial 1.0.0 bundles
// (AC1); this file is the installation-side half, which runs on every boot
// regardless of whether that one-time publish, or any registry, has ever
// been reached — it records each embedded provider as its own initially-
// activated version and mints/records tool_ ids for its already-embedded
// tools (AC2), so a fresh install with no registry configured still gets
// stable tool_ id addressing (PD65/PD68).
package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"beecon/internal/registrybundle"
)

// embeddedSeedInitialVersion is the version BackfillEmbeddedSeed records for
// a provider it has never seen an ActivatedDefinition row for — mirroring
// registryservice's own "a provider's first bundle is 1.0.0" rule (Slice 1),
// since this is exactly that: the embedded seed's own initial version.
const embeddedSeedInitialVersion = "1.0.0"

// BackfillEmbeddedSeed records every embedded provider definition this
// facade was constructed with, and has never been activated through the
// registry (no ActivatedDefinition row on file for its slug), as that
// provider's initially-activated version, minting a tool_ id for each of its
// tools that doesn't already have one (AC2). It is idempotent (AC3): a
// provider already carrying an ActivatedDefinition row — whether written by
// a real Activate call or by an earlier run of this same backfill — is left
// untouched, so re-running after every provider has caught up mints nothing
// new. It returns how many tool_ ids it minted this run (including zero),
// the same count app/wiring.go logs at boot, mirroring
// EncryptPlaintextClientSecrets' own boot-backfill convention. A facade with
// no ActivatedDefinitions store or tool_ id minter wired (WithRegistrySync
// never called, or called with a nil newToolID) treats this as a no-op,
// exactly like LoadActivatedDefinitions.
func (f *Facade) BackfillEmbeddedSeed(ctx context.Context) (int, error) {
	if f.activatedDefinitions == nil || f.newToolID == nil {
		return 0, nil
	}

	stampedCount := 0
	for _, definition := range sortedDefinitionsByProviderSlug(f.definitionsSnapshot()) {
		stamped, err := f.backfillOneProvider(ctx, definition)
		if err != nil {
			return 0, err
		}
		stampedCount += stamped
	}

	slog.Default().Info(fmt.Sprintf("stamped %d tool_ ids for the embedded provider seed", stampedCount),
		"count", stampedCount)
	return stampedCount, nil
}

// backfillOneProvider backfills a single provider definition, returning how
// many of its tools this call minted a tool_ id for (0 when this provider
// already has an ActivatedDefinition row, or already carried a tool_ id on
// every tool).
func (f *Facade) backfillOneProvider(ctx context.Context, definition ProviderDefinition) (int, error) {
	existing, err := f.activatedDefinitions.FindByProviderSlug(ctx, definition.Slug)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, nil
	}

	stamped, mintedCount := stampMissingToolIDs(definition, f.newToolID)

	bundle := BundleFromProviderDefinition(stamped)
	bundle.Version = embeddedSeedInitialVersion
	contentHash, err := registrybundle.ContentHash(bundle)
	if err != nil {
		return 0, err
	}
	bundle.ContentHash = contentHash

	if err := f.persistActivatedDefinition(ctx, definition.Slug, bundle); err != nil {
		return 0, err
	}
	f.setDefinition(stamped)
	return mintedCount, nil
}

// stampMissingToolIDs returns a copy of definition with a freshly minted
// tool_ id assigned to every tool whose ID is still empty (an embedded tool
// that has never been through the registry), alongside how many it minted.
// A tool that already carries an ID (impossible for a purely embedded
// definition today, but kept safe for a future embedded seed that ships one
// pre-assigned) is left untouched.
func stampMissingToolIDs(definition ProviderDefinition, newToolID func() string) (ProviderDefinition, int) {
	minted := 0
	tools := make([]ProviderTool, len(definition.Tools))
	for i, tool := range definition.Tools {
		if tool.ID == "" {
			tool.ID = newToolID()
			minted++
		}
		tools[i] = tool
	}
	definition.Tools = tools
	return definition, minted
}

// sortedDefinitionsByProviderSlug returns definitions in deterministic slug
// order — definitionsSnapshot's own map has no ordering, and a deterministic
// order keeps this backfill's behavior (and its logged count) reproducible
// across runs regardless of Go's randomized map iteration.
func sortedDefinitionsByProviderSlug(definitions map[string]ProviderDefinition) []ProviderDefinition {
	sorted := make([]ProviderDefinition, 0, len(definitions))
	for _, definition := range definitions {
		sorted = append(sorted, definition)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Slug < sorted[j].Slug })
	return sorted
}
