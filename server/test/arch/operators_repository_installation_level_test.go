package arch

import (
	"reflect"
	"testing"

	"beecon/internal/access"
)

// TestAccessOperatorsRepository_WouldFailOrgScopingIfNotWhitelisted documents
// and pins exactly why access.Operators sits in
// TestInstallationLevelPortsAreExplicitlyWhitelisted (orgscope_test.go)
// rather than being checked by the per-org-scoped tests above it (Phase 5
// Slice 1, PD49/PD58): Exists takes only a context.Context (no second
// parameter at all), and FindByEmail/FindByID both take a raw string-derived
// id, not organizations.OrgID or a struct carrying one — so running the raw
// org-scope checker directly against access.Operators (bypassing the
// whitelist) must flag every method. That confirms the whitelist entry is a
// deliberate "an Operator administers the whole installation, like the admin
// key it replaces — there is no organization to scope by" call, not an
// oversight that would silently start passing if this port's shape ever
// regressed to something scope-shaped by accident.
func TestAccessOperatorsRepository_WouldFailOrgScopingIfNotWhitelisted(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.Operators)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected access.Operators to fail the raw org-scope checker (Exists takes no second parameter at all, FindByEmail/FindByID take a raw string/OperatorID, not OrgID) — if this ever passes, either the checker regressed or this port accidentally became org-scoped, and the whitelist entry in TestInstallationLevelPortsAreExplicitlyWhitelisted should be reconsidered")
	}
}

// TestAccessOperatorSessionsRepository_WouldFailOrgScopingIfNotWhitelisted is
// the same pinning test as above, one credential class later (PD51): a
// session belongs to one operator, never to an organization — FindByTokenHash
// takes a raw []byte hash, and Save takes an OperatorSession that carries no
// OrgID field at all.
func TestAccessOperatorSessionsRepository_WouldFailOrgScopingIfNotWhitelisted(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*access.OperatorSessions)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected access.OperatorSessions to fail the raw org-scope checker (no method's identifying parameter is organizations.OrgID or a struct carrying an OrgID field) — if this ever passes, either the checker regressed or this port accidentally became org-scoped, and the whitelist entry in TestInstallationLevelPortsAreExplicitlyWhitelisted should be reconsidered")
	}
}
