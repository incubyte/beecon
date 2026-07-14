package arch

import (
	"reflect"
	"testing"

	"beecon/internal/organizations"
)

// TestOrganizationsRepository_ListAllWouldFailOrgScopingIfNotWhitelisted
// documents, and pins, exactly why organizations.Repository sits in
// TestInstallationLevelPortsAreExplicitlyWhitelisted (orgscope_test.go)
// rather than being checked by the per-org-scoped tests above it (Slice 1,
// PD40): ListAll's own second parameter is *organizations.ListAllCursor —
// neither organizations.OrgID nor a struct carrying an OrgID field — so
// running the raw org-scope checker directly against
// organizations.Repository (bypassing the whitelist) must flag it. That
// confirms the whitelist entry is a deliberate "Organization IS the
// isolation unit, there is no wider scope to filter by" call, not an
// oversight that would silently start passing if ListAll's signature ever
// regressed to something scope-shaped by accident.
func TestOrganizationsRepository_ListAllWouldFailOrgScopingIfNotWhitelisted(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*organizations.Repository)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected organizations.Repository to fail the raw org-scope checker (ListAll takes *ListAllCursor, not OrgID) — if this ever passes, either the checker regressed or ListAll's signature accidentally became org-scoped, and the whitelist entry in TestInstallationLevelPortsAreExplicitlyWhitelisted should be reconsidered")
	}
}
