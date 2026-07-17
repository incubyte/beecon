// Phase 5 registry sub-phase, Slice 3 ("review before adopting"): an
// installation operator can list the versions the registry offers for a
// provider and see exactly what a target version would add, change, or
// remove relative to the version currently active in this installation —
// pulled from the registry and applied to nothing (activation, Slice 1's
// Facade.Activate, stays the only place the served catalog changes). The
// diff is computed here, installation-side (PD64: pull/diff/activate all
// live in catalog/ behind the RegistryClient port), independently of
// registryservice's own bundlediff.go — the two are separate deployables
// sharing only the registrybundle wire format (BOUNDARIES.md), and this
// diff additionally distinguishes "changed" (a schema edit) from
// "added"/"removed", which registryservice's publish-time bump-direction
// enforcement never needed.
package catalog

import (
	"context"
	"reflect"
	"sort"

	"beecon/internal/registrybundle"
)

// ListRegistryVersions returns every version the registry offers for
// providerSlug, each marked active/not against this installation's
// currently activated version (Slice 3's first AC) — an operator reviews
// this before requesting a diff or activating anything. Returns
// ErrRegistryNotConfigured when this facade was never given a
// RegistryClient (mirrors Activate).
func (f *Facade) ListRegistryVersions(ctx context.Context, providerSlug string) (RegistryVersionsView, error) {
	if f.registryClient == nil {
		return RegistryVersionsView{}, ErrRegistryNotConfigured()
	}
	versions, err := f.registryClient.ListVersions(ctx, providerSlug)
	if err != nil {
		return RegistryVersionsView{}, err
	}
	activeVersion, err := f.activeVersionFor(ctx, providerSlug)
	if err != nil {
		return RegistryVersionsView{}, err
	}
	items := make([]RegistryVersionSummary, 0, len(versions))
	for _, version := range versions {
		items = append(items, RegistryVersionSummary{Version: version, Active: version == activeVersion})
	}
	return RegistryVersionsView{Items: items, ActiveVersion: activeVersion}, nil
}

// DiffRegistryVersion pulls toVersion from the registry and reports what it
// would add, change, or remove relative to the version currently active in
// this installation (Slice 3's second AC). Nothing pulled here is activated
// or persisted, so the active version and the served catalog stay exactly
// as they were (AC4). An unreachable registry surfaces
// ErrRegistryUnavailable (AC5); a version the registry does not offer
// surfaces ErrBundleVersionNotFound (AC6).
func (f *Facade) DiffRegistryVersion(ctx context.Context, providerSlug, toVersion string) (RegistryDiff, error) {
	if f.registryClient == nil {
		return RegistryDiff{}, ErrRegistryNotConfigured()
	}
	if toVersion == "" {
		return RegistryDiff{}, ErrValidation("to", "must not be empty")
	}

	to, err := f.registryClient.FetchBundle(ctx, providerSlug, toVersion)
	if err != nil {
		return RegistryDiff{}, err
	}

	from, err := f.activeBundleFor(ctx, providerSlug)
	if err != nil {
		return RegistryDiff{}, err
	}

	diff := diffRegistryBundles(from, to)
	diff.From = activeVersionOf(from)
	diff.To = toVersion
	return diff, nil
}

// activeVersionFor returns providerSlug's currently activated version in
// this installation, or "" when it has never been activated through the
// registry (or this facade has no ActivatedDefinitions store wired at all).
func (f *Facade) activeVersionFor(ctx context.Context, providerSlug string) (string, error) {
	bundle, err := f.activeBundleFor(ctx, providerSlug)
	if err != nil {
		return "", err
	}
	return activeVersionOf(bundle), nil
}

// activeBundleFor returns providerSlug's currently activated bundle, or nil
// when none has ever been activated through the registry — diffRegistryBundles
// treats a nil from exactly like a provider that has never been activated:
// everything in to counts as added.
func (f *Facade) activeBundleFor(ctx context.Context, providerSlug string) (*registrybundle.Bundle, error) {
	if f.activatedDefinitions == nil {
		return nil, nil
	}
	activated, err := f.activatedDefinitions.FindByProviderSlug(ctx, providerSlug)
	if err != nil {
		return nil, err
	}
	if activated == nil {
		return nil, nil
	}
	bundle, err := decodeBundleJSON(activated.BundleJSON)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}

func activeVersionOf(bundle *registrybundle.Bundle) string {
	if bundle == nil {
		return ""
	}
	return bundle.Version
}

// diffRegistryBundles computes from -> to's added, changed, and removed
// tool and trigger slugs (Slice 3): from is nil for a provider never
// activated through the registry, in which case every slug in to counts as
// added.
func diffRegistryBundles(from *registrybundle.Bundle, to registrybundle.Bundle) RegistryDiff {
	addedTools, changedTools, removedTools := diffToolSlugs(toolsBySlug(from), toolsBySlug(&to))
	addedTriggers, changedTriggers, removedTriggers := diffTriggerSlugs(triggersBySlug(from), triggersBySlug(&to))
	return RegistryDiff{
		Added:   RegistryDiffItem{Tools: addedTools, Triggers: addedTriggers},
		Changed: RegistryDiffItem{Tools: changedTools, Triggers: changedTriggers},
		Removed: RegistryDiffItem{Tools: removedTools, Triggers: removedTriggers},
	}
}

func toolsBySlug(bundle *registrybundle.Bundle) map[string]registrybundle.Tool {
	bySlug := make(map[string]registrybundle.Tool)
	if bundle == nil {
		return bySlug
	}
	for _, tool := range bundle.Tools {
		bySlug[tool.Slug] = tool
	}
	return bySlug
}

func triggersBySlug(bundle *registrybundle.Bundle) map[string]registrybundle.Trigger {
	bySlug := make(map[string]registrybundle.Trigger)
	if bundle == nil {
		return bySlug
	}
	for _, trigger := range bundle.Triggers {
		bySlug[trigger.Slug] = trigger
	}
	return bySlug
}

// diffToolSlugs returns from -> to's added, changed, and removed tool
// slugs, sorted for deterministic output: a tool present in both counts as
// changed when its input or output schema differs (AC3), added when it
// only appears in to, removed when it only appears in from.
func diffToolSlugs(from, to map[string]registrybundle.Tool) (added, changed, removed []string) {
	for slug, toTool := range to {
		fromTool, existed := from[slug]
		switch {
		case !existed:
			added = append(added, slug)
		case toolSchemaChanged(fromTool, toTool):
			changed = append(changed, slug)
		}
	}
	for slug := range from {
		if _, stillPresent := to[slug]; !stillPresent {
			removed = append(removed, slug)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed
}

func toolSchemaChanged(from, to registrybundle.Tool) bool {
	return !reflect.DeepEqual(from.InputSchema, to.InputSchema) || !reflect.DeepEqual(from.OutputSchema, to.OutputSchema)
}

// diffTriggerSlugs mirrors diffToolSlugs for triggers: a trigger present in
// both counts as changed when its config or payload schema differs.
func diffTriggerSlugs(from, to map[string]registrybundle.Trigger) (added, changed, removed []string) {
	for slug, toTrigger := range to {
		fromTrigger, existed := from[slug]
		switch {
		case !existed:
			added = append(added, slug)
		case triggerSchemaChanged(fromTrigger, toTrigger):
			changed = append(changed, slug)
		}
	}
	for slug := range from {
		if _, stillPresent := to[slug]; !stillPresent {
			removed = append(removed, slug)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed
}

func triggerSchemaChanged(from, to registrybundle.Trigger) bool {
	return !reflect.DeepEqual(from.ConfigSchema, to.ConfigSchema) || !reflect.DeepEqual(from.PayloadSchema, to.PayloadSchema)
}
