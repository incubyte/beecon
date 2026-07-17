// registry_activation_trigger_pause_test.go (package catalog_test) exercises
// Slice 4's dependent-trigger-instance safety net (PD66): a trigger removed
// by a newly-activated version pauses every organization's live
// TriggerInstances bound to it (StatusPausedTriggerRemoved), those instances
// stop being claimable by the poller (triggers.PollQueue.ClaimDuePolls), a
// provider's very first activation never calls the pauser at all (nothing
// yet to diff against), and re-pausing an already-paused instance is a
// silent no-op. Wires catalog.Facade's TriggerInstancePauser to a real
// triggers.Facade over a shared in-memory Repository — the same
// app/recorders.go composition-root shape production wiring uses, minus the
// app package itself (catalog does not depend on triggers; only a
// composition root may cross that boundary, and a test file is exactly that
// kind of neutral ground).
package catalog_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/connections"
	"beecon/internal/organizations"
	"beecon/internal/registrybundle"
	"beecon/internal/triggers"
	triggersmemory "beecon/internal/triggers/driven/memory"
)

// triggersPauserAdapter adapts *triggers.Facade to catalog.TriggerInstancePauser
// — mirrors app/recorders.go's catalogTriggerInstancePauser, kept local to
// this test file since catalog's own production code must never import
// triggers (BOUNDARIES: the dependency runs the other way).
type triggersPauserAdapter struct{ triggers *triggers.Facade }

func (a triggersPauserAdapter) PauseInstancesForRemovedTrigger(ctx context.Context, triggerSlug string) error {
	return a.triggers.PauseInstancesForRemovedTrigger(ctx, triggerSlug)
}

var _ catalog.TriggerInstancePauser = triggersPauserAdapter{}

func TestActivate_ATriggerRemovedByTheNewVersionPausesEveryOrgsLiveInstancesAndTheyBecomeUnclaimableByThePoller(t *testing.T) {
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	claimNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const leaseTTL = time.Minute

	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0",
		[]registrybundle.Tool{activationTool("outlook-list-messages")},
		[]registrybundle.Trigger{activationTrigger("outlook-message-received")},
	))
	client.Seed("outlook", activationBundle("2.0.0",
		[]registrybundle.Tool{activationTool("outlook-list-messages")},
		nil, // 2.0.0 removes the trigger
	))

	instanceRepo := triggersmemory.NewRepository()
	triggersFacade := triggersmemory.NewFacadeWithOverrides(triggersmemory.Overrides{Repository: instanceRepo})

	catalogFacade, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
		TriggerInstancePauser: triggersPauserAdapter{triggers: triggersFacade},
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := catalogFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}

	orgA := organizations.OrgID("org_a")
	orgB := organizations.OrgID("org_b")
	instanceA := triggers.NewTriggerInstance("trg_a", orgA, "user_a", connections.ConnectionID("conn_a"), "outlook-message-received", nil, past)
	instanceB := triggers.NewTriggerInstance("trg_b", orgB, "user_b", connections.ConnectionID("conn_b"), "outlook-message-received", nil, past)
	if err := instanceRepo.Save(context.Background(), instanceA); err != nil {
		t.Fatalf("seed instanceA: %v", err)
	}
	if err := instanceRepo.Save(context.Background(), instanceB); err != nil {
		t.Fatalf("seed instanceB: %v", err)
	}

	if _, err := catalogFacade.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0: %v", err)
	}

	updatedA, err := instanceRepo.FindByID(context.Background(), orgA, instanceA.ID)
	if err != nil {
		t.Fatalf("FindByID orgA: %v", err)
	}
	if updatedA.Status != triggers.StatusPausedTriggerRemoved {
		t.Errorf("orgA's instance status = %q, want %q", updatedA.Status, triggers.StatusPausedTriggerRemoved)
	}
	updatedB, err := instanceRepo.FindByID(context.Background(), orgB, instanceB.ID)
	if err != nil {
		t.Fatalf("FindByID orgB: %v", err)
	}
	if updatedB.Status != triggers.StatusPausedTriggerRemoved {
		t.Errorf("orgB's instance status = %q, want %q", updatedB.Status, triggers.StatusPausedTriggerRemoved)
	}

	due, err := instanceRepo.ClaimDuePolls(context.Background(), claimNow, leaseTTL, 10)
	if err != nil {
		t.Fatalf("ClaimDuePolls: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("ClaimDuePolls claimed %d instances, want 0 — paused instances must not be claimable by the poller: %+v", len(due), due)
	}

	// Idempotent: re-pausing an already-paused instance is a silent no-op,
	// not an error, and does not un-pause or otherwise mutate it.
	if err := triggersFacade.PauseInstancesForRemovedTrigger(context.Background(), "outlook-message-received"); err != nil {
		t.Errorf("re-pausing an already-paused trigger slug must be a no-op, got error: %v", err)
	}
	stillA, err := instanceRepo.FindByID(context.Background(), orgA, instanceA.ID)
	if err != nil {
		t.Fatalf("FindByID orgA (after re-pause): %v", err)
	}
	if stillA.Status != triggers.StatusPausedTriggerRemoved {
		t.Errorf("orgA's instance status after a redundant pause = %q, want it to remain %q", stillA.Status, triggers.StatusPausedTriggerRemoved)
	}
}

func TestActivate_AProvidersFirstEverActivationNeverCallsTheTriggerInstancePauser(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("acme-crm", activationBundleForProvider("acme-crm", "1.0.0", nil, []registrybundle.Trigger{activationTrigger("acme-deal-updated")}))
	pauser := catalogmemory.NewTriggerInstancePauser()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
		TriggerInstancePauser: pauser,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}

	if _, err := f.Activate(context.Background(), "acme-crm", "1.0.0"); err != nil {
		t.Fatalf("Activate acme-crm 1.0.0: %v", err)
	}

	if paused := pauser.Paused(); len(paused) != 0 {
		t.Errorf("a provider's first-ever activation must never call the trigger-instance pauser, got %v", paused)
	}
}
