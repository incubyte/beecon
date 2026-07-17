package registryservice

import (
	"sort"

	"beecon/internal/registrybundle"
)

// bundleDiff is what changed, by slug, between a provider's previously
// published bundle and the one just submitted to Publish (PD62's semver
// bump-direction rule needs to know whether anything was added or removed;
// Slice 2's last AC needs the same sets reported back in the publish
// response). A provider's first publish diffs against an empty bundle, so
// everything in it counts as added.
type bundleDiff struct {
	addedTools, removedTools       []string
	addedTriggers, removedTriggers []string
}

func (d bundleDiff) hasAdditions() bool {
	return len(d.addedTools) > 0 || len(d.addedTriggers) > 0
}

func (d bundleDiff) hasRemovals() bool {
	return len(d.removedTools) > 0 || len(d.removedTriggers) > 0
}

// diffBundle computes prior -> next's added/removed tool and trigger slugs.
// prior is nil for a provider's first publish, in which case every slug in
// next counts as added.
func diffBundle(prior *registrybundle.Bundle, next registrybundle.Bundle) bundleDiff {
	addedTools, removedTools := diffSlugs(priorToolSlugs(prior), toolSlugs(next.Tools))
	addedTriggers, removedTriggers := diffSlugs(priorTriggerSlugs(prior), triggerSlugs(next.Triggers))
	return bundleDiff{
		addedTools: addedTools, removedTools: removedTools,
		addedTriggers: addedTriggers, removedTriggers: removedTriggers,
	}
}

func priorToolSlugs(prior *registrybundle.Bundle) []string {
	if prior == nil {
		return nil
	}
	return toolSlugs(prior.Tools)
}

func priorTriggerSlugs(prior *registrybundle.Bundle) []string {
	if prior == nil {
		return nil
	}
	return triggerSlugs(prior.Triggers)
}

func toolSlugs(tools []registrybundle.Tool) []string {
	slugs := make([]string, len(tools))
	for i, tool := range tools {
		slugs[i] = tool.Slug
	}
	return slugs
}

func triggerSlugs(triggers []registrybundle.Trigger) []string {
	slugs := make([]string, len(triggers))
	for i, trigger := range triggers {
		slugs[i] = trigger.Slug
	}
	return slugs
}

// diffSlugs returns, sorted for deterministic output, the slugs present in
// next but not prior (added) and present in prior but not next (removed).
func diffSlugs(prior, next []string) (added, removed []string) {
	priorSet := toSlugSet(prior)
	nextSet := toSlugSet(next)
	for slug := range nextSet {
		if !priorSet[slug] {
			added = append(added, slug)
		}
	}
	for slug := range priorSet {
		if !nextSet[slug] {
			removed = append(removed, slug)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func toSlugSet(slugs []string) map[string]bool {
	set := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		set[slug] = true
	}
	return set
}
