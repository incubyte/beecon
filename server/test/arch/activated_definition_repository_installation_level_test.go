package arch

import (
	"reflect"
	"testing"

	"beecon/internal/catalog"
)

// TestCatalogActivatedDefinitionsRepository_WouldFailOrgScopingIfNotWhitelisted
// documents and pins exactly why catalog.ActivatedDefinitions sits in
// TestInstallationLevelPortsAreExplicitlyWhitelisted (orgscope_test.go)
// rather than being checked by the per-org-scoped tests above it (Phase 5
// registry sub-phase Slice 1, PD65): FindByProviderSlug takes a raw
// provider slug, not organizations.OrgID or a struct carrying one, and
// Save's ActivatedDefinition argument carries no OrgID field at all — so
// running the raw org-scope checker directly against
// catalog.ActivatedDefinitions (bypassing the whitelist) must flag every
// method. That confirms the whitelist entry is a deliberate "a provider's
// activated definition is installation-wide, like catalog.Repository
// itself (PD7) — there is no organization to scope by" call, not an
// oversight that would silently start passing if this port's shape ever
// regressed to something scope-shaped by accident.
func TestCatalogActivatedDefinitionsRepository_WouldFailOrgScopingIfNotWhitelisted(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*catalog.ActivatedDefinitions)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected catalog.ActivatedDefinitions to fail the raw org-scope checker (FindByProviderSlug takes a raw string, Save's ActivatedDefinition carries no OrgID field) — if this ever passes, either the checker regressed or this port accidentally became org-scoped, and the whitelist entry in TestInstallationLevelPortsAreExplicitlyWhitelisted should be reconsidered")
	}
}
