// Package registryservice is the domain of Beecon's separate tool-registry
// service (Phase 5 registry sub-phase, PD59): a standalone deployable
// (cmd/registry) sharing only the registrybundle wire format with the
// installation binary. It publishes provider bundles — minting each tool's
// immutable tool_ id (PD61) and assigning a semver version (PD62) — and
// serves them back to authenticated installations to pull. It depends on
// no domain module.
package registryservice

import (
	"time"

	"beecon/internal/registrybundle"
)

// StoredBundle is one published bundle version as the registry's Store
// persists it (PD60: git-backed behind a storage port — Slice 1's concrete
// adapter, driven/diskstore, is a plain filesystem JSON store; a real
// git-backed adapter, git init + commit per publish, is a later hardening
// step behind this same port).
type StoredBundle struct {
	Bundle      registrybundle.Bundle
	PublishedAt time.Time
}

// ToolIdentity is one tool's registry-minted identity, returned in the
// publish response (Slice 1 AC): the immutable tool_ id alongside its slug
// (PD61).
type ToolIdentity struct {
	ID   string
	Slug string
}

// BundleDiffItem names the tools and triggers (by slug) a publish added or
// removed relative to the provider's previously published version (Slice 2's
// last AC). Both slices are nil on a provider's first publish, since there
// is no previous version to diff against.
type BundleDiffItem struct {
	Tools    []string
	Triggers []string
}

// BundleVersionSummary is one version a provider has published, as
// Facade.ListVersions returns it (Slice 3): enough for an installation
// operator to review what versions exist before pulling or activating one.
type BundleVersionSummary struct {
	Version     string
	ContentHash string
	PublishedAt time.Time
}

// PublishResult is what Facade.Publish returns: the assigned version, the
// bundle's content hash (PD62), every tool's minted identity (Slice 1), and
// — Slice 2 — what changed relative to the provider's previous version.
type PublishResult struct {
	Version     string
	ContentHash string
	Tools       []ToolIdentity
	Added       BundleDiffItem
	Removed     BundleDiffItem
}
