// tool_id_addressing_test.go (package execution_test, reuses testOrg/
// testUser/testConnectionID/testToolSlug/activeConnectionReader/
// fakeProviderClient/fakeRecorder/messagesResponse/fixedClock/assertDomainError
// from facade_test.go) exercises the Phase 5 registry sub-phase's Slice 5
// AC2/AC3/AC5: Execute treats a caller-supplied tool_ id exactly the same way
// it treats a slug — same resolved tool, same Result, and a log entry that
// names the RESOLVED tool's own id and slug regardless of which handle the
// caller addressed it by — and an unknown tool_ id is a platform not-found,
// never a tool-level failure.
package execution_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/execution"
)

const testToolID = "tool_execution_addressing"

// idOrSlugToolReader is a hand-written execution.ToolReader for a single
// registered tool, resolving it by either its slug or its immutable tool_ id
// — mirroring catalog.Facade.FindToolBySlug's own "additive to slug"
// resolution (ADR-0006) — so Execute's caller-handle-independence can be
// proven without depending on the real catalog module.
type idOrSlugToolReader struct {
	tool catalog.ProviderTool
}

func (r idOrSlugToolReader) FindToolBySlug(_ context.Context, idOrSlug string) (catalog.ProviderDefinition, catalog.ProviderTool, error) {
	if idOrSlug == r.tool.ID || idOrSlug == r.tool.Slug {
		return catalog.ProviderDefinition{Slug: "outlook"}, r.tool, nil
	}
	return catalog.ProviderDefinition{}, catalog.ProviderTool{}, catalog.ErrToolNotFound()
}

func addressableTool() catalog.ProviderTool {
	return catalog.ProviderTool{
		ID: testToolID, Slug: testToolSlug, Method: "GET",
		Path: "https://graph.microsoft.com/v1.0/me/messages",
	}
}

// TestExecute_ByToolIDAndBySlugResolveToTheSameToolAndProduceTheSameResult is
// AC3: whichever handle the caller used, Execute must produce an identical
// {successful, error, data} result shape.
func TestExecute_ByToolIDAndBySlugResolveToTheSameToolAndProduceTheSameResult(t *testing.T) {
	toolReader := idOrSlugToolReader{tool: addressableTool()}

	fBySlug := execution.NewFacade(toolReader, activeConnectionReader(), &fakeProviderClient{response: messagesResponse()}, nil, fixedClock(time.Now()))
	resultBySlug, err := fBySlug.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{})
	if err != nil {
		t.Fatalf("Execute(slug): %v", err)
	}

	fByID := execution.NewFacade(toolReader, activeConnectionReader(), &fakeProviderClient{response: messagesResponse()}, nil, fixedClock(time.Now()))
	resultByID, err := fByID.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolID, map[string]any{})
	if err != nil {
		t.Fatalf("Execute(tool_ id): %v", err)
	}

	if !reflect.DeepEqual(resultBySlug, resultByID) {
		t.Errorf("Execute by slug = %+v, Execute by tool_ id = %+v, want identical results", resultBySlug, resultByID)
	}
}

// TestExecute_ByToolIDAndBySlugWriteIdenticalLogAttributionNamingTheResolvedTool
// is the identical-log-attribution guarantee: a log entry always names the
// RESOLVED tool's own ToolID/ToolSlug, never the raw identifier the caller
// happened to address it by.
func TestExecute_ByToolIDAndBySlugWriteIdenticalLogAttributionNamingTheResolvedTool(t *testing.T) {
	toolReader := idOrSlugToolReader{tool: addressableTool()}

	recorderForSlugCall := &fakeRecorder{}
	fBySlug := execution.NewFacade(toolReader, activeConnectionReader(), &fakeProviderClient{response: messagesResponse()}, recorderForSlugCall, fixedClock(time.Now()))
	if _, err := fBySlug.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{}); err != nil {
		t.Fatalf("Execute(slug): %v", err)
	}

	recorderForIDCall := &fakeRecorder{}
	fByID := execution.NewFacade(toolReader, activeConnectionReader(), &fakeProviderClient{response: messagesResponse()}, recorderForIDCall, fixedClock(time.Now()))
	if _, err := fByID.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolID, map[string]any{}); err != nil {
		t.Fatalf("Execute(tool_ id): %v", err)
	}

	if len(recorderForSlugCall.entries) != 1 || len(recorderForIDCall.entries) != 1 {
		t.Fatalf("expected exactly one recorded entry per call, got %d (addressed by slug) and %d (addressed by tool_ id)",
			len(recorderForSlugCall.entries), len(recorderForIDCall.entries))
	}
	entryFromSlugCall := recorderForSlugCall.entries[0]
	entryFromIDCall := recorderForIDCall.entries[0]

	if entryFromSlugCall.ToolID != testToolID || entryFromSlugCall.ToolSlug != testToolSlug {
		t.Errorf("addressed by slug: recorded ToolID=%q ToolSlug=%q, want the resolved tool's own %q/%q",
			entryFromSlugCall.ToolID, entryFromSlugCall.ToolSlug, testToolID, testToolSlug)
	}
	if entryFromIDCall.ToolID != testToolID || entryFromIDCall.ToolSlug != testToolSlug {
		t.Errorf("addressed by tool_ id: recorded ToolID=%q ToolSlug=%q, want the resolved tool's own %q/%q",
			entryFromIDCall.ToolID, entryFromIDCall.ToolSlug, testToolID, testToolSlug)
	}
}

// TestExecute_AnUnknownToolIDIsANotFoundErrorDistinctFromAToolLevelExecutionFailure
// is AC5: an unknown tool_ id must surface as the same platform not-found an
// unknown slug already produces (PD6), never a {successful:false} tool-level
// Result, and must never reach the provider.
func TestExecute_AnUnknownToolIDIsANotFoundErrorDistinctFromAToolLevelExecutionFailure(t *testing.T) {
	toolReader := idOrSlugToolReader{tool: addressableTool()}
	provider := &fakeProviderClient{}
	f := execution.NewFacade(toolReader, activeConnectionReader(), provider, nil, fixedClock(time.Now()))

	result, err := f.Execute(context.Background(), testOrg, testUser, testConnectionID, "tool_never_minted", map[string]any{})

	assertDomainError(t, err, catalog.CodeNotFound, 404)
	if result.Successful || result.Error != nil {
		t.Errorf("Execute with an unknown tool_ id returned Result %+v, want the zero Result alongside a platform error (never a tool-level failure)", result)
	}
	if provider.callCount != 0 {
		t.Errorf("provider was called %d times for an unknown tool_ id, want 0", provider.callCount)
	}
}
