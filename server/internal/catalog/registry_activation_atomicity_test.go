// registry_activation_atomicity_test.go (package catalog_test, reuses
// fakeDefinitions/assertDomainError from facade_test.go and the
// activation*/versionedTool builders from registry_activation_fixtures_test.go)
// exercises Slice 4's core HIGH-risk invariant: Activate is atomic. A bundle
// that fails validation must leave the previously active version fully in
// force with no partial swap, and a bundle that passes validation but whose
// later dependent-safety step (pausing removed-trigger instances) fails must
// be rolled back completely — the served definition and the persisted
// activated-definition row both restored to exactly what they were before
// the call started.
package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"beecon/internal/catalog"
	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/registrybundle"
)

// --- Validation failure leaves state untouched (AC1) ---

func TestActivate_AContentHashMismatchLeavesThePreviouslyActivatedDefinitionAndRowByteIdenticalAndReturnsNoBundleData(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil))
	tampered := activationBundle("1.1.0", []registrybundle.Tool{
		activationTool("outlook-list-messages"), activationTool("outlook-get-message"),
	}, nil)
	tampered.ContentHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	client.Seed("outlook", tampered)
	activated := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: activated,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	beforeDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (before): %v", err)
	}
	beforeRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (before): %v", err)
	}

	result, activateErr := f.Activate(context.Background(), "outlook", "1.1.0")

	assertDomainError(t, activateErr, catalog.CodeContentHashMismatch, 422)
	if result != (catalog.ActivatedVersion{}) {
		t.Errorf("Activate returned bundle data (%+v) alongside its error, want the zero value", result)
	}
	afterDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (after): %v", err)
	}
	if !reflect.DeepEqual(beforeDefinition, afterDefinition) {
		t.Errorf("served definition changed after a rejected content-hash mismatch:\nbefore=%+v\nafter=%+v", beforeDefinition, afterDefinition)
	}
	afterRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (after): %v", err)
	}
	if !reflect.DeepEqual(beforeRow, afterRow) {
		t.Errorf("activated definition row changed after a rejected content-hash mismatch:\nbefore=%+v\nafter=%+v", beforeRow, afterRow)
	}
}

func TestActivate_AnUnsupportedFormatVersionLeavesThePreviouslyActivatedDefinitionAndRowByteIdentical(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil))
	unsupported := activationBundle("2.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil)
	unsupported.FormatVersion = 2
	client.Seed("outlook", unsupported)
	activated := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: activated,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	beforeDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (before): %v", err)
	}
	beforeRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (before): %v", err)
	}

	_, activateErr := f.Activate(context.Background(), "outlook", "2.0.0")

	assertDomainError(t, activateErr, catalog.CodeUnsupportedFormatVersion, 422)
	afterDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (after): %v", err)
	}
	if !reflect.DeepEqual(beforeDefinition, afterDefinition) {
		t.Errorf("served definition changed after a rejected unsupported formatVersion:\nbefore=%+v\nafter=%+v", beforeDefinition, afterDefinition)
	}
	afterRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (after): %v", err)
	}
	if !reflect.DeepEqual(beforeRow, afterRow) {
		t.Errorf("activated definition row changed after a rejected unsupported formatVersion:\nbefore=%+v\nafter=%+v", beforeRow, afterRow)
	}
}

// --- Rollback on a later dependent-safety failure (AC2) ---

func TestActivate_APauserFailureRollsBackTheServedDefinitionAndTheActivatedRowToThePreviousVersion(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0",
		[]registrybundle.Tool{activationTool("outlook-list-messages")},
		[]registrybundle.Trigger{activationTrigger("outlook-message-received")},
	))
	client.Seed("outlook", activationBundle("2.0.0",
		[]registrybundle.Tool{activationTool("outlook-list-messages")},
		nil, // 2.0.0 removes the trigger 1.0.0 declared
	))
	pauser := catalogmemory.NewTriggerInstancePauser()
	activated := catalogmemory.NewActivatedDefinitionRepository()
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: activated, TriggerInstancePauser: pauser,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	beforeDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (before): %v", err)
	}
	beforeRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (before): %v", err)
	}
	pauseErr := errors.New("pausing outlook-message-received instances failed")
	pauser.FailOnSlug("outlook-message-received", pauseErr)

	_, activateErr := f.Activate(context.Background(), "outlook", "2.0.0")

	if !errors.Is(activateErr, pauseErr) {
		t.Fatalf("Activate error = %v, want it to surface the pauser's own failure (%v)", activateErr, pauseErr)
	}
	afterDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (after): %v", err)
	}
	if !reflect.DeepEqual(beforeDefinition, afterDefinition) {
		t.Errorf("served definition not rolled back to 1.0.0 after the pauser failed:\nbefore=%+v\nafter=%+v", beforeDefinition, afterDefinition)
	}
	afterRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (after): %v", err)
	}
	if !reflect.DeepEqual(beforeRow, afterRow) {
		t.Errorf("activated definition row not rolled back to 1.0.0 after the pauser failed:\nbefore=%+v\nafter=%+v", beforeRow, afterRow)
	}
}

// TestActivate_APauserFailureOnAProvidersFirstRegistryActivationDeletesTheJustWrittenRowAndRestoresTheEmbeddedDefinition
// exercises rollbackActivation's other branch (registry_sync.go): when the
// provider being activated had never been recorded in the DB-backed
// ActivatedDefinitions store before this call (previousActivated nil), a
// rollback must delete the row this call just wrote rather than restore some
// earlier one — there is no earlier one. This still requires the trigger
// pauser to actually run, which only happens when this installation was
// already serving a definition for the provider before this call
// (hadPreviousDefinition true) — here, the embedded seed already declares the
// trigger the pulled bundle removes, so the served definition rolls back to
// that embedded seed while the never-before-existing DB row is deleted
// outright.
func TestActivate_APauserFailureOnAProvidersFirstRegistryActivationDeletesTheJustWrittenRowAndRestoresTheEmbeddedDefinition(t *testing.T) {
	embeddedSeedWithATrigger := []catalog.ProviderDefinition{
		{
			Slug: "outlook", Name: "Outlook", AuthScheme: "oauth2",
			AuthorizeURL: "https://example.com/authorize", TokenURL: "https://example.com/token", Scopes: []string{"Mail.Read"},
			Triggers: []catalog.TriggerDefinition{
				{Slug: "outlook-message-received", Name: "outlook-message-received", ConfigSchema: minimalSchema(), PayloadSchema: minimalSchema(), Ingestion: "poll"},
			},
		},
	}
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil)) // no trigger: removes the embedded seed's one
	pauser := catalogmemory.NewTriggerInstancePauser()
	activated := catalogmemory.NewActivatedDefinitionRepository() // empty: outlook has never been activated through the registry before
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: embeddedSeedWithATrigger, RegistryClient: client, ActivatedDefinitions: activated, TriggerInstancePauser: pauser,
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	beforeDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (before): %v", err)
	}
	pauseErr := errors.New("pausing outlook-message-received instances failed")
	pauser.FailOnSlug("outlook-message-received", pauseErr)

	_, activateErr := f.Activate(context.Background(), "outlook", "1.0.0")

	if !errors.Is(activateErr, pauseErr) {
		t.Fatalf("Activate error = %v, want it to surface the pauser's own failure (%v)", activateErr, pauseErr)
	}
	afterDefinition, err := f.GetProviderDefinition(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("GetProviderDefinition (after): %v", err)
	}
	if !reflect.DeepEqual(beforeDefinition, afterDefinition) {
		t.Errorf("served definition not rolled back to the embedded seed:\nbefore=%+v\nafter=%+v", beforeDefinition, afterDefinition)
	}
	afterRow, err := activated.FindByProviderSlug(context.Background(), "outlook")
	if err != nil {
		t.Fatalf("FindByProviderSlug (after): %v", err)
	}
	if afterRow != nil {
		t.Errorf("activated definition row = %+v, want it deleted (this provider had never been activated through the registry before this failed call)", afterRow)
	}
}

// --- Rollback = activating a previous version again (AC7) ---

func TestActivate_ReactivatingAPriorVersionRestoresItsToolsWithNoSpecialRollbackCode(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil))
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{
		activationTool("outlook-list-messages"), activationTool("outlook-get-message"),
	}, nil))
	f, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}
	if _, err := f.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0: %v", err)
	}

	activated, err := f.Activate(context.Background(), "outlook", "1.0.0")

	if err != nil {
		t.Fatalf("re-activating 1.0.0: %v", err)
	}
	if activated.ActiveVersion != "1.0.0" {
		t.Errorf("ActiveVersion = %q, want %q", activated.ActiveVersion, "1.0.0")
	}
	_, listTool, err := f.FindToolBySlug(context.Background(), "outlook-list-messages")
	if err != nil {
		t.Fatalf("FindToolBySlug(outlook-list-messages): %v", err)
	}
	if listTool.Deprecated {
		t.Errorf("outlook-list-messages must be active (non-deprecated) after reactivating 1.0.0, got %+v", listTool)
	}
	_, getMessageTool, err := f.FindToolBySlug(context.Background(), "outlook-get-message")
	if err != nil {
		t.Fatalf("FindToolBySlug(outlook-get-message): %v", err)
	}
	if !getMessageTool.Deprecated {
		t.Errorf("outlook-get-message (only declared by 2.0.0) must be carried forward as deprecated after reactivating 1.0.0, got %+v", getMessageTool)
	}
}
