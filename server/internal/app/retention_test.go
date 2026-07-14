// retention_test.go exercises retentionReader (retention.go), the
// composition-root adapter satisfying both logging.RetentionReader and
// delivery.RetentionReader (Slice 7, PD44): ListOrgIDs walking
// organizations.Facade.ListAll to completion, and Effective*RetentionDays
// resolving an org's own governance override against the installation
// default via organizations.Facade.GetGovernance — the one seam that
// combines config.BEECON_RETENTION_DAYS with an org's own override without
// either logging or delivery importing organizations/config directly.
package app

import (
	"context"
	"testing"

	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

func TestRetentionReader_ListOrgIDsReturnsEveryOrganizationAcrossPages(t *testing.T) {
	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	seedOrg(t, orgFacade, "Acme")
	seedOrg(t, orgFacade, "Globex")
	seedOrg(t, orgFacade, "Initech")
	reader := retentionReader{organizations: orgFacade, installationDefaultDays: 30}

	ids, err := reader.ListOrgIDs(context.Background())

	if err != nil {
		t.Fatalf("ListOrgIDs: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("ListOrgIDs returned %d ids, want 3", len(ids))
	}
}

func TestRetentionReader_EffectiveLogRetentionDaysInheritsTheInstallationDefaultWhenUnset(t *testing.T) {
	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	org := seedOrg(t, orgFacade, "Acme")
	reader := retentionReader{organizations: orgFacade, installationDefaultDays: 45}

	got, err := reader.EffectiveLogRetentionDays(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("EffectiveLogRetentionDays: %v", err)
	}
	if got != 45 {
		t.Errorf("EffectiveLogRetentionDays = %d, want the installation default 45", got)
	}
}

func TestRetentionReader_EffectiveLogRetentionDaysUsesTheOrgsOwnOverrideWhenSet(t *testing.T) {
	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	org := seedOrg(t, orgFacade, "Acme")
	logDays := 7
	if _, err := orgFacade.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{LogRetentionDays: &logDays}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	reader := retentionReader{organizations: orgFacade, installationDefaultDays: 45}

	got, err := reader.EffectiveLogRetentionDays(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("EffectiveLogRetentionDays: %v", err)
	}
	if got != 7 {
		t.Errorf("EffectiveLogRetentionDays = %d, want the org's own override 7", got)
	}
}

func TestRetentionReader_EffectiveEventRetentionDaysMirrorsTheSameResolution(t *testing.T) {
	orgFacade := memory.NewFacadeWithOverrides(memory.Overrides{})
	org := seedOrg(t, orgFacade, "Acme")
	eventDays := 0
	if _, err := orgFacade.SetRetention(context.Background(), org.ID, organizations.RetentionUpdate{EventRetentionDays: &eventDays}); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	reader := retentionReader{organizations: orgFacade, installationDefaultDays: 45}

	got, err := reader.EffectiveEventRetentionDays(context.Background(), org.ID)

	if err != nil {
		t.Fatalf("EffectiveEventRetentionDays: %v", err)
	}
	if got != 0 {
		t.Errorf("EffectiveEventRetentionDays = %d, want the org's own explicit 0 (unlimited), not the installation default", got)
	}
}
