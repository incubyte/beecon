package arch

import (
	"reflect"
	"testing"

	"beecon/internal/triggers"
)

// TestTriggersTriggerSlugIndex_WouldFailOrgScopingIfNotWhitelisted documents
// and pins exactly why triggers.TriggerSlugIndex sits in
// TestInstallationLevelPortsAreExplicitlyWhitelisted (orgscope_test.go)
// rather than being checked by the per-org-scoped tests above it (Phase 5
// registry sub-phase Slice 4, PD66): ListByTriggerSlug takes a raw
// triggerSlug string, not organizations.OrgID or a struct carrying one — so
// running the raw org-scope checker directly against
// triggers.TriggerSlugIndex (bypassing the whitelist) must flag it. That
// confirms the whitelist entry is a deliberate "PauseInstancesForRemovedTrigger
// scans every organization's instances bound to one trigger slug in a single
// shared operation, mirroring triggers.PollQueue's own cross-org poller scan
// — there is no single organization to scope this query by" call, not an
// oversight that would silently start passing if this port's shape ever
// regressed to something scope-shaped by accident.
func TestTriggersTriggerSlugIndex_WouldFailOrgScopingIfNotWhitelisted(t *testing.T) {
	got := orgScopeViolations(reflect.TypeOf((*triggers.TriggerSlugIndex)(nil)).Elem())

	if len(got) == 0 {
		t.Fatal("expected triggers.TriggerSlugIndex to fail the raw org-scope checker (ListByTriggerSlug takes a raw string, not organizations.OrgID or a struct carrying an OrgID field) — if this ever passes, either the checker regressed or this port accidentally became org-scoped, and the whitelist entry in TestInstallationLevelPortsAreExplicitlyWhitelisted should be reconsidered")
	}
}
