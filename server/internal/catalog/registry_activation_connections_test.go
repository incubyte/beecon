// registry_activation_connections_test.go (package catalog_test) exercises
// Slice 4's AC that activating a new version leaves existing connections
// (OAuth credentials) for that provider completely untouched: a Connection
// belongs to a stable provider identity (slug/IntegrationID), never to a
// definition version, and lives in a wholly separate module/repository
// Activate has no dependency on at all.
package catalog_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/connections"
	connectionsmemory "beecon/internal/connections/driven/memory"
	"beecon/internal/organizations"
	"beecon/internal/registrybundle"
)

func TestActivate_LeavesAnExistingConnectionForThatProviderCompletelyUnchanged(t *testing.T) {
	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{activationTool("outlook-list-messages")}, nil))
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{
		activationTool("outlook-list-messages"), activationTool("outlook-get-message"),
	}, nil))
	catalogFacade, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := catalogFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}

	integrationSummary, err := catalogFacade.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("CreateIntegration: %v", err)
	}

	connRepo := connectionsmemory.NewRepository()
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	org := organizations.OrgID("org_1")
	connection := connections.NewConnection(
		connections.ConnectionID("conn_1"), org, organizations.UserID("user_1"),
		integrationSummary.ID, "outlook", "https://example.com/redirect", "connect_token_1", fixedTime,
	)
	activatedConnection := connection.Activate("encrypted-access-token", "encrypted-refresh-token", "user@example.com", "A User", fixedTime.Add(time.Hour))
	if err := connRepo.Save(context.Background(), activatedConnection); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	before, err := connRepo.FindByID(context.Background(), org, activatedConnection.ID)
	if err != nil {
		t.Fatalf("FindByID (before): %v", err)
	}

	if _, err := catalogFacade.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0: %v", err)
	}

	after, err := connRepo.FindByID(context.Background(), org, activatedConnection.ID)
	if err != nil {
		t.Fatalf("FindByID (after): %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("connection changed after an unrelated provider activation:\nbefore=%+v\nafter=%+v", before, after)
	}
}
