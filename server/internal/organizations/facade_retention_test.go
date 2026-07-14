// facade_retention_test.go exercises organizations.Governance's own
// retention predicates (WithRetention/Effective*RetentionDays) and
// Facade.GetRetention/SetRetention (Slice 7, PD44) — mirrors
// facade_governance_test.go's own split for the governance half of the same
// org_governance settings row (FD8). See retention_handler_test.go for the
// HTTP boundary and delivery/logging's own facade_purge_test.go for
// PurgeOnce's consumption of EffectiveLogRetentionDays/
// EffectiveEventRetentionDays.
package organizations_test

import (
	"context"
	"testing"

	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

func intPtr(v int) *int { return &v }

// --- Governance.EffectiveLogRetentionDays / EffectiveEventRetentionDays ---

func TestEffectiveLogRetentionDays_NilInheritsTheInstallationDefault(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	if got := g.EffectiveLogRetentionDays(30); got != 30 {
		t.Errorf("EffectiveLogRetentionDays = %d, want 30 (the installation default, unset override)", got)
	}
}

func TestEffectiveLogRetentionDays_ZeroMeansUnlimitedNotTheInstallationDefault(t *testing.T) {
	g, err := organizations.NewDefaultGovernance("org_1").WithRetention(intPtr(0), nil)
	if err != nil {
		t.Fatalf("WithRetention: %v", err)
	}

	if got := g.EffectiveLogRetentionDays(30); got != 0 {
		t.Errorf("EffectiveLogRetentionDays = %d, want 0 — an explicit 0 must override the installation default, not fall back to it", got)
	}
}

func TestEffectiveLogRetentionDays_AnExplicitPositiveValueOverridesTheInstallationDefault(t *testing.T) {
	g, err := organizations.NewDefaultGovernance("org_1").WithRetention(intPtr(7), nil)
	if err != nil {
		t.Fatalf("WithRetention: %v", err)
	}

	if got := g.EffectiveLogRetentionDays(30); got != 7 {
		t.Errorf("EffectiveLogRetentionDays = %d, want 7 (the org's own override)", got)
	}
}

func TestEffectiveEventRetentionDays_MirrorsTheSameThreeCases(t *testing.T) {
	inherited := organizations.NewDefaultGovernance("org_1")
	if got := inherited.EffectiveEventRetentionDays(30); got != 30 {
		t.Errorf("nil override: EffectiveEventRetentionDays = %d, want 30", got)
	}

	unlimited, err := inherited.WithRetention(nil, intPtr(0))
	if err != nil {
		t.Fatalf("WithRetention(unlimited): %v", err)
	}
	if got := unlimited.EffectiveEventRetentionDays(30); got != 0 {
		t.Errorf("0 override: EffectiveEventRetentionDays = %d, want 0", got)
	}

	overridden, err := inherited.WithRetention(nil, intPtr(14))
	if err != nil {
		t.Fatalf("WithRetention(14): %v", err)
	}
	if got := overridden.EffectiveEventRetentionDays(30); got != 14 {
		t.Errorf("explicit override: EffectiveEventRetentionDays = %d, want 14", got)
	}
}

// --- Governance.WithRetention validation (MinRetentionDays, 0 exempt) ---

func TestWithRetention_RejectsAValueBelowTheMinimumButNotZero(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	if _, err := g.WithRetention(intPtr(0), nil); err != nil {
		t.Errorf("WithRetention(0): unexpected error %v — 0 (unlimited) must always be accepted", err)
	}
	for _, tooLow := range []int{-1, -100} {
		if _, err := g.WithRetention(intPtr(tooLow), nil); err == nil {
			t.Errorf("WithRetention(%d): expected a validation error, got nil", tooLow)
		}
	}
}

func TestWithRetention_AcceptsExactlyTheMinimum(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	updated, err := g.WithRetention(intPtr(organizations.MinRetentionDays), nil)

	if err != nil {
		t.Fatalf("WithRetention(MinRetentionDays): unexpected error: %v", err)
	}
	if updated.LogRetentionDays == nil || *updated.LogRetentionDays != organizations.MinRetentionDays {
		t.Errorf("LogRetentionDays = %v, want %d", updated.LogRetentionDays, organizations.MinRetentionDays)
	}
}

func TestWithRetention_ValidatesLogAndEventWindowsIndependently(t *testing.T) {
	g := organizations.NewDefaultGovernance("org_1")

	// A valid log window alongside an invalid event window must still be
	// rejected — WithRetention validates both fields, not just the first.
	_, err := g.WithRetention(intPtr(30), intPtr(-1))

	if err == nil {
		t.Fatal("expected a validation error for an invalid eventRetentionDays even with a valid logRetentionDays, got nil")
	}
}

func TestWithRetention_LeavesGovernanceHalfOfTheRecordUntouched(t *testing.T) {
	allowList := []string{"intg_1"}
	g, err := organizations.NewGovernance("org_1", &allowList, []string{"intg_2"}, []string{"intg_1"}, 5)
	if err != nil {
		t.Fatalf("NewGovernance: %v", err)
	}

	updated, err := g.WithRetention(intPtr(10), intPtr(20))

	if err != nil {
		t.Fatalf("WithRetention: %v", err)
	}
	if updated.AllowList == nil || len(*updated.AllowList) != 1 || (*updated.AllowList)[0] != "intg_1" {
		t.Errorf("AllowList = %v, want unchanged [intg_1]", updated.AllowList)
	}
	if len(updated.Hidden) != 1 || updated.Hidden[0] != "intg_2" {
		t.Errorf("Hidden = %v, want unchanged [intg_2]", updated.Hidden)
	}
	if updated.FeaturedCap != 5 {
		t.Errorf("FeaturedCap = %d, want unchanged 5", updated.FeaturedCap)
	}
}

// --- Facade.GetRetention / SetRetention ---

func TestGetRetention_AnOrganizationWithNoRetentionSetSeesBothWindowsNil(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := f.GetRetention(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.LogRetentionDays != nil {
		t.Errorf("LogRetentionDays = %v, want nil (inherit the installation default)", got.LogRetentionDays)
	}
	if got.EventRetentionDays != nil {
		t.Errorf("EventRetentionDays = %v, want nil", got.EventRetentionDays)
	}
}

func TestSetRetention_PersistsAndGetRetentionRoundTripsBothWindows(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	saved, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{
		LogRetentionDays:   intPtr(14),
		EventRetentionDays: intPtr(0),
	})
	if err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if saved.LogRetentionDays == nil || *saved.LogRetentionDays != 14 {
		t.Errorf("SetRetention result LogRetentionDays = %v, want 14", saved.LogRetentionDays)
	}
	if saved.EventRetentionDays == nil || *saved.EventRetentionDays != 0 {
		t.Errorf("SetRetention result EventRetentionDays = %v, want 0 (unlimited)", saved.EventRetentionDays)
	}

	got, err := f.GetRetention(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.LogRetentionDays == nil || *got.LogRetentionDays != 14 {
		t.Errorf("LogRetentionDays = %v, want 14", got.LogRetentionDays)
	}
	if got.EventRetentionDays == nil || *got.EventRetentionDays != 0 {
		t.Errorf("EventRetentionDays = %v, want 0", got.EventRetentionDays)
	}
}

// TestSetRetention_RejectsAWindowBelowTheMinimumAndWritesNothing is AC5:
// a sub-minimum window (and not 0, which is exempt) is rejected with a
// validation error, and the org's previous retention state is left
// untouched.
func TestSetRetention_RejectsAWindowBelowTheMinimumAndWritesNothing(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(10)}); err != nil {
		t.Fatalf("initial SetRetention: %v", err)
	}

	_, err = f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(-1)})

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)

	got, gerr := f.GetRetention(context.Background(), org.ID)
	if gerr != nil {
		t.Fatalf("GetRetention: %v", gerr)
	}
	if got.LogRetentionDays == nil || *got.LogRetentionDays != 10 {
		t.Errorf("LogRetentionDays after a rejected SetRetention = %v, want the previous value 10 untouched", got.LogRetentionDays)
	}
}

// TestSetRetention_AcceptsZeroAsUnlimitedDespiteTheMinimum is AC4/AC5
// together: 0 is always accepted even though it is below MinRetentionDays,
// because it names a distinct "unlimited" state, not a too-short window.
func TestSetRetention_AcceptsZeroAsUnlimitedDespiteTheMinimum(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	saved, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{
		LogRetentionDays:   intPtr(0),
		EventRetentionDays: intPtr(0),
	})

	if err != nil {
		t.Fatalf("SetRetention(0, 0): unexpected error: %v", err)
	}
	if saved.LogRetentionDays == nil || *saved.LogRetentionDays != 0 {
		t.Errorf("LogRetentionDays = %v, want 0", saved.LogRetentionDays)
	}
	if saved.EventRetentionDays == nil || *saved.EventRetentionDays != 0 {
		t.Errorf("EventRetentionDays = %v, want 0", saved.EventRetentionDays)
	}
}

// TestSetRetention_ASecondCallReplacesTheWholeRetentionRecord pins the
// whole-replace convention (mirrors SetGovernance's own): a second
// SetRetention with a nil field must clear a previously-set value, not
// leave it untouched.
func TestSetRetention_ASecondCallReplacesTheWholeRetentionRecord(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(14)}); err != nil {
		t.Fatalf("first SetRetention: %v", err)
	}

	if _, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: nil}); err != nil {
		t.Fatalf("second SetRetention: %v", err)
	}

	got, err := f.GetRetention(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.LogRetentionDays != nil {
		t.Errorf("LogRetentionDays = %v, want nil after replacing with an unset window", got.LogRetentionDays)
	}
}

// TestSetRetention_LeavesGovernanceUntouched and
// TestSetGovernance_LeavesRetentionUntouched are FD8's own cross-halves
// guarantee at the facade seam: org_governance is one shared settings row,
// but SetRetention and SetGovernance must each replace only their own half.
func TestSetRetention_LeavesGovernanceUntouched(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	allowList := []string{"intg_1"}
	if _, err := f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{AllowList: &allowList}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}

	if _, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(14)}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}

	got, err := f.GetGovernance(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("GetGovernance: %v", err)
	}
	if got.AllowList == nil || len(*got.AllowList) != 1 || (*got.AllowList)[0] != "intg_1" {
		t.Errorf("AllowList after SetRetention = %v, want the previously-set [intg_1] untouched", got.AllowList)
	}
}

func TestSetGovernance_LeavesRetentionUntouched(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	org, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(14)}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}

	allowList := []string{"intg_1"}
	if _, err := f.SetGovernance(context.Background(), org.ID, organizations.GovernanceUpdate{AllowList: &allowList}); err != nil {
		t.Fatalf("SetGovernance: %v", err)
	}

	got, err := f.GetRetention(context.Background(), org.ID)
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if got.LogRetentionDays == nil || *got.LogRetentionDays != 14 {
		t.Errorf("LogRetentionDays after SetGovernance = %v, want the previously-set 14 untouched", got.LogRetentionDays)
	}
}

// TestGetRetention_IsStrictlyOrgScoped_TwoOrganizationsRetentionNeverCrosses
// mirrors facade_governance_test.go's own isolation pin, for retention.
func TestGetRetention_IsStrictlyOrgScoped_TwoOrganizationsRetentionNeverCrosses(t *testing.T) {
	f := memory.NewFacadeWithOverrides(memory.Overrides{})
	orgA, err := f.Create(context.Background(), "Org A")
	if err != nil {
		t.Fatalf("Create org A: %v", err)
	}
	orgB, err := f.Create(context.Background(), "Org B")
	if err != nil {
		t.Fatalf("Create org B: %v", err)
	}
	if _, err := f.SetRetention(context.Background(), orgA.ID, organizations.RetentionUpdate{LogRetentionDays: intPtr(1)}); err != nil {
		t.Fatalf("SetRetention org A: %v", err)
	}

	gotB, err := f.GetRetention(context.Background(), orgB.ID)

	if err != nil {
		t.Fatalf("GetRetention org B: %v", err)
	}
	if gotB.LogRetentionDays != nil {
		t.Errorf("org B's LogRetentionDays = %v, want nil — org A's retention window must never leak across", gotB.LogRetentionDays)
	}
}
