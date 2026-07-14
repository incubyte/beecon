// facade_governance_test.go exercises organizations.Governance's own domain
// predicates (IsVisible/IsHidden), NewGovernance's validation and tri-state
// copy semantics, and Facade.GetGovernance/SetGovernance (Slice 5,
// PD42/PD43) — the core-risk seam's read/write half that catalog.Facade's
// GovernanceReader port calls through. See governance_visibility_test.go in
// the catalog package for the seam's actual enforcement (filtering
// integrations/tools/trigger-definitions), and
// governance_handler_test.go/connections' facade_governance_test.go for the
// HTTP and cross-module halves.
package organizations_test

import (
	"context"
	"testing"

	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

// --- Governance.IsVisible/IsHidden: pure domain predicates ---

func TestGovernance_IsVisible_NilAllowListMeansEveryNonHiddenIntegrationIsVisible(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	if !g.IsVisible("intg_anything") {
		t.Error("IsVisible = false, want true — a nil allow-list (PD42's continuity default) must not restrict anything")
	}
}

func TestGovernance_IsVisible_HiddenAlwaysWinsEvenWithNoAllowList(t *testing.T) {
	g, err := organizations.NewGovernance("org_1", nil, []string{"intg_hidden"}, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if g.IsVisible("intg_hidden") {
		t.Error("IsVisible = true, want false — Hidden must win even when AllowList is nil (inherit-all)")
	}
	if !g.IsVisible("intg_other") {
		t.Error("IsVisible = false for a non-hidden integration, want true (nil allow-list still inherits everything else)")
	}
}

func TestGovernance_IsVisible_WithAnAllowListOnlyListedIntegrationsAreVisible(t *testing.T) {
	allowList := []string{"intg_allowed"}
	g, err := organizations.NewGovernance("org_1", &allowList, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if !g.IsVisible("intg_allowed") {
		t.Error("IsVisible(intg_allowed) = false, want true — it is on the allow-list")
	}
	if g.IsVisible("intg_not_allowed") {
		t.Error("IsVisible(intg_not_allowed) = true, want false — an allow-list present must exclude anything not listed")
	}
}

func TestGovernance_IsVisible_AnEmptyNonNilAllowListHidesEveryIntegration(t *testing.T) {
	allowList := []string{}
	g, err := organizations.NewGovernance("org_1", &allowList, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if g.IsVisible("intg_anything") {
		t.Error("IsVisible = true, want false — a present-but-empty allow-list (distinct from nil) allows nothing")
	}
}

func TestGovernance_IsVisible_HiddenWinsOverAnExplicitAllowListEntry(t *testing.T) {
	allowList := []string{"intg_x"}
	g, err := organizations.NewGovernance("org_1", &allowList, []string{"intg_x"}, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if g.IsVisible("intg_x") {
		t.Error("IsVisible = true, want false — an operator hiding an allow-listed integration must still hide it")
	}
}

func TestGovernance_IsHidden_ReportsMembershipInTheHiddenSetOnly(t *testing.T) {
	g, err := organizations.NewGovernance("org_1", nil, []string{"intg_hidden"}, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if !g.IsHidden("intg_hidden") {
		t.Error("IsHidden(intg_hidden) = false, want true")
	}
	if g.IsHidden("intg_other") {
		t.Error("IsHidden(intg_other) = true, want false")
	}
}

// --- NewDefaultGovernance / NewGovernance construction ---

func TestNewDefaultGovernance_HasNoAllowListNothingHiddenNothingFeaturedAndThePlatformFeaturedCap(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	if g.AllowList != nil {
		t.Errorf("AllowList = %v, want nil (PD42's continuity default)", g.AllowList)
	}
	if len(g.Hidden) != 0 {
		t.Errorf("Hidden = %v, want empty", g.Hidden)
	}
	if len(g.Featured) != 0 {
		t.Errorf("Featured = %v, want empty", g.Featured)
	}
	if g.FeaturedCap != organizations.DefaultFeaturedCap {
		t.Errorf("FeaturedCap = %d, want %d", g.FeaturedCap, organizations.DefaultFeaturedCap)
	}
}

func TestNewGovernance_AppliesTheDefaultCapWhenFeaturedCapIsUnsetOrNonPositive(t *testing.T) {
	for _, cap := range []int{0, -1, -100} {
		g, err := organizations.NewGovernance("org_1", nil, nil, nil, cap)
		if err != nil {
			t.Fatalf("NewGovernance(cap=%d): %v", cap, err)
		}
		if g.FeaturedCap != organizations.DefaultFeaturedCap {
			t.Errorf("cap input %d: FeaturedCap = %d, want the platform default %d", cap, g.FeaturedCap, organizations.DefaultFeaturedCap)
		}
	}
}

func TestNewGovernance_RejectsAFeaturedListLongerThanTheEffectiveCap(t *testing.T) {
	_, err := organizations.NewGovernance("org_1", nil, nil, []string{"a", "b", "c"}, 2)

	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
}

func TestNewGovernance_AcceptsAFeaturedListExactlyAtTheCap(t *testing.T) {
	g, err := organizations.NewGovernance("org_1", nil, nil, []string{"a", "b"}, 2)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(g.Featured) != 2 {
		t.Errorf("Featured = %v, want length 2", g.Featured)
	}
}

// TestNewGovernance_MutatingTheCallersAllowListSliceAfterConstructionDoesNotAffectTheStoredValue
// pins NewGovernance's defensive copy (copyAllowList/copyStrings): the
// stored Governance must not alias the caller's own slice.
func TestNewGovernance_MutatingTheCallersAllowListSliceAfterConstructionDoesNotAffectTheStoredValue(t *testing.T) {
	allowList := []string{"intg_1"}
	g, err := organizations.NewGovernance("org_1", &allowList, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	allowList[0] = "intg_mutated"

	if (*g.AllowList)[0] != "intg_1" {
		t.Errorf("stored AllowList = %v, want it unaffected by mutating the caller's original slice", *g.AllowList)
	}
}

func TestNewGovernance_ANilAllowListStaysNilNotAnEmptySlice(t *testing.T) {
	g, err := organizations.NewGovernance("org_1", nil, nil, nil, 0)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	if g.AllowList != nil {
		t.Errorf("AllowList = %v, want nil preserved, not coerced to an empty slice", g.AllowList)
	}
}

// --- Facade.GetGovernance / SetGovernance ---

// TestGetGovernance_AnOrganizationWithNoGovernanceRowSeesTheContinuityPreservingDefault
// is PD42's core continuity guarantee at the facade seam: an organization
// that has never had SetGovernance called synthesizes NewDefaultGovernance
// rather than erroring or returning a zero value.
func TestGetGovernance_AnOrganizationWithNoGovernanceRowSeesTheContinuityPreservingDefault(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.GetGovernance(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := organizations.NewDefaultGovernance(org.ID)
	if got.AllowList != nil {
		t.Errorf("AllowList = %v, want nil", got.AllowList)
	}
	if len(got.Hidden) != 0 || len(got.Featured) != 0 {
		t.Errorf("Hidden/Featured = %v/%v, want both empty", got.Hidden, got.Featured)
	}
	if got.FeaturedCap != want.FeaturedCap {
		t.Errorf("FeaturedCap = %d, want %d", got.FeaturedCap, want.FeaturedCap)
	}
}

func TestSetGovernance_PersistsAndGetGovernanceRoundTripsTheExactValues(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	allowList := []string{"intg_1", "intg_2"}

	saved, err := f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{
		AllowList:   &allowList,
		Hidden:      []string{"intg_3"},
		Featured:    []string{"intg_1"},
		FeaturedCap: 5,
	})
	if err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}
	if saved.FeaturedCap != 5 {
		t.Errorf("SetGovernance result FeaturedCap = %d, want 5", saved.FeaturedCap)
	}

	got, err := f.GetGovernance(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if got.AllowList == nil || len(*got.AllowList) != 2 || (*got.AllowList)[0] != "intg_1" || (*got.AllowList)[1] != "intg_2" {
		t.Errorf("AllowList = %v, want [intg_1 intg_2]", got.AllowList)
	}
	if len(got.Hidden) != 1 || got.Hidden[0] != "intg_3" {
		t.Errorf("Hidden = %v, want [intg_3]", got.Hidden)
	}
	if len(got.Featured) != 1 || got.Featured[0] != "intg_1" {
		t.Errorf("Featured = %v, want [intg_1]", got.Featured)
	}
	if got.FeaturedCap != 5 {
		t.Errorf("FeaturedCap = %d, want 5", got.FeaturedCap)
	}
}

// TestSetGovernance_ASecondCallReplacesTheWholeRecordRatherThanMerging pins
// the whole-replace convention (mirrors SetAllowedRedirectURIs): a second
// SetGovernance with a nil AllowList must clear a previously-set one, not
// leave it untouched.
func TestSetGovernance_ASecondCallReplacesTheWholeRecordRatherThanMerging(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	firstAllowList := []string{"intg_1"}
	if _, err := f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{AllowList: &firstAllowList}); err != nil {
		t.Fatalf("first SetGovernance: %v", err)
	}

	if _, err := f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{AllowList: nil}); err != nil {
		t.Fatalf("second SetGovernance: %v", err)
	}

	got, err := f.GetGovernance(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if got.AllowList != nil {
		t.Errorf("AllowList = %v, want nil after replacing with an unset allow-list", got.AllowList)
	}
}

func TestSetGovernance_RejectsAFeaturedListExceedingTheCapAndWritesNothing(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{
		Featured:    []string{"a", "b", "c"},
		FeaturedCap: 2,
	})

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)

	got, gerr := f.GetGovernance(context.Background(), org.ID)
	if gerr != nil {
		t.Fatalf("GetGovernance: %v", gerr)
	}
	if len(got.Featured) != 0 {
		t.Errorf("Featured = %v after a rejected SetGovernance, want the previous (empty) state untouched", got.Featured)
	}
}

// TestGetGovernance_IsStrictlyOrgScoped_TwoOrganizationsGovernanceNeverCrosses
// is the org-scoping half of Slice 5's isolation AC at the persistence
// seam itself: configuring one organization's governance must never be
// observable through another organization's GetGovernance call.
func TestGetGovernance_IsStrictlyOrgScoped_TwoOrganizationsGovernanceNeverCrosses(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	orgA, err := f.Create(context.Background(), "Org A")
	if err != nil {
		t.Fatalf("Create org A: %v", err)
	}
	orgB, err := f.Create(context.Background(), "Org B")
	if err != nil {
		t.Fatalf("Create org B: %v", err)
	}
	allowListA := []string{"intg_a_only"}
	if _, err := f.SetGovernance(context.Background(), orgA.ID, organizations.GovernanceUpdate{AllowList: &allowListA}); err != nil {
		t.Fatalf("SetGovernance org A: %v", err)
	}

	gotB, err := f.GetGovernance(context.Background(), orgB.ID)

	if err != nil {
		t.Fatalf("GetGovernance org B: %v", err)
	}
	if gotB.AllowList != nil {
		t.Errorf("org B's AllowList = %v, want nil — org A's governance must never leak across", gotB.AllowList)
	}
}
