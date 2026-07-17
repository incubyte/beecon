// registry_activation_inflight_test.go (package catalog_test) exercises
// Slice 4's concurrency-sensitive AC: an execution already in flight when a
// version is activated must complete against the tool definition it started
// on, never a definition swapped in mid-call. execution.Facade.Execute
// resolves FindToolBySlug once, copying the tool's fields under
// catalog.Facade's RLock before ever calling the provider (facade.go's own
// documented invariant); setDefinition installs a wholesale new value rather
// than mutating one in place. barrierProviderClient makes this deterministic
// — the test only proceeds past the barrier once it knows the in-flight call
// has already resolved and built its request, never relying on a sleep race.
package catalog_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	catalogmemory "beecon/internal/catalog/driven/memory"
	"beecon/internal/connections"
	"beecon/internal/execution"
	"beecon/internal/organizations"
	"beecon/internal/registrybundle"
)

// barrierProviderClient blocks inside Call until release is closed, closing
// called first — a deterministic barrier for a concurrency test, not a sleep
// race.
type barrierProviderClient struct {
	called  chan struct{}
	release chan struct{}
}

func (b *barrierProviderClient) Call(_ context.Context, req execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	close(b.called)
	<-b.release
	return execution.ToolCallResponse{StatusCode: 200, Body: fmt.Sprintf(`{"calledURL":%q}`, req.URL)}, nil
}

// alwaysActiveConnectionReader is a minimal execution.ConnectionReader stand-in:
// every connection resolves ACTIVE with a fixed access token — the
// in-flight-execution test only needs Execute to reach the provider, never a
// non-ACTIVE or refresh path.
type alwaysActiveConnectionReader struct{}

func (alwaysActiveConnectionReader) ResolveForExecution(_ context.Context, _ organizations.OrgID, _ organizations.UserID, _ connections.ConnectionID) (connections.ExecutionAccess, error) {
	return connections.ExecutionAccess{Status: connections.StatusActive, AccessToken: "test-access-token"}, nil
}

func (alwaysActiveConnectionReader) RefreshForExecution(_ context.Context, _ organizations.OrgID, _ organizations.UserID, _ connections.ConnectionID) (connections.ExecutionAccess, error) {
	return connections.ExecutionAccess{Status: connections.StatusActive, AccessToken: "test-access-token"}, nil
}

type inflightExecuteOutcome struct {
	result execution.Result
	err    error
}

func TestExecute_AnInFlightCallCompletesAgainstTheDefinitionVersionItStartedOnDespiteAConcurrentActivation(t *testing.T) {
	const org = organizations.OrgID("org_inflight")
	const user = organizations.UserID("user_inflight")
	const connID = connections.ConnectionID("conn_inflight")

	client := catalogmemory.NewRegistryClient()
	client.Seed("outlook", activationBundle("1.0.0", []registrybundle.Tool{versionedTool("outlook-list-messages", "/v1.0/messages")}, nil))
	client.Seed("outlook", activationBundle("2.0.0", []registrybundle.Tool{versionedTool("outlook-list-messages", "/v2.0/messages")}, nil))
	catalogFacade, err := catalogmemory.NewFacadeWithOverrides(catalogmemory.Overrides{
		Definitions: fakeDefinitions(), RegistryClient: client, ActivatedDefinitions: catalogmemory.NewActivatedDefinitionRepository(),
	})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	if _, err := catalogFacade.Activate(context.Background(), "outlook", "1.0.0"); err != nil {
		t.Fatalf("Activate 1.0.0: %v", err)
	}

	provider := &barrierProviderClient{called: make(chan struct{}), release: make(chan struct{})}
	execFacade := execution.NewFacade(catalogFacade, alwaysActiveConnectionReader{}, provider, nil, func() time.Time { return time.Unix(0, 0) })

	outcomes := make(chan inflightExecuteOutcome, 1)
	go func() {
		result, err := execFacade.Execute(context.Background(), org, user, connID, "outlook-list-messages", map[string]any{})
		outcomes <- inflightExecuteOutcome{result: result, err: err}
	}()

	select {
	case <-provider.called:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the in-flight call to reach the provider")
	}

	if _, err := catalogFacade.Activate(context.Background(), "outlook", "2.0.0"); err != nil {
		t.Fatalf("Activate 2.0.0 (concurrent with the in-flight call): %v", err)
	}
	close(provider.release)

	var outcome inflightExecuteOutcome
	select {
	case outcome = <-outcomes:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the in-flight Execute call to complete")
	}

	if outcome.err != nil {
		t.Fatalf("Execute: %v", outcome.err)
	}
	if !outcome.result.Successful {
		t.Fatalf("result = %+v, want a successful result", outcome.result)
	}
	data, ok := outcome.result.Data.(map[string]any)
	if !ok {
		t.Fatalf("result.Data = %#v, want a decoded JSON object", outcome.result.Data)
	}
	calledURL, _ := data["calledURL"].(string)
	if !strings.Contains(calledURL, "/v1.0/messages") {
		t.Errorf("calledURL = %q, want the in-flight call to have used the v1.0/messages path it started on", calledURL)
	}
	if strings.Contains(calledURL, "/v2.0/messages") {
		t.Errorf("calledURL = %q, must not reflect the version activated mid-flight", calledURL)
	}
}
