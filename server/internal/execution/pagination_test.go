// White-box (package execution) tests for pagination.go's unexported
// applyPaginationQuery and extractNextCursor: PD15b's canonical
// pageSize/cursor -> provider-param mapping, and the dotted-path nextCursor
// extraction out of a decoded provider response (Hubspot's
// "paging.next.after", the coder's flagged gap — these were previously only
// exercised indirectly through Facade.Execute).
package execution

import (
	"testing"

	"beecon/internal/catalog"
)

func TestApplyPaginationQuery_MapsCanonicalPageSizeAndCursorToTheProvidersOwnParamNames(t *testing.T) {
	pagination := &catalog.Pagination{PageSizeParam: "limit", CursorParam: "after"}
	query := map[string]string{}

	applyPaginationQuery(query, pagination, map[string]any{"pageSize": float64(25), "cursor": "cursor-abc"})

	if query["limit"] != "25" {
		t.Errorf(`query["limit"] = %q, want "25"`, query["limit"])
	}
	if query["after"] != "cursor-abc" {
		t.Errorf(`query["after"] = %q, want "cursor-abc"`, query["after"])
	}
}

// TestApplyPaginationQuery_OmitsAnArgumentTheCallerDidNotSupply proves a
// caller who paginates without a cursor (the first page) must not send an
// empty/zero-value cursor param to the provider.
func TestApplyPaginationQuery_OmitsAnArgumentTheCallerDidNotSupply(t *testing.T) {
	pagination := &catalog.Pagination{PageSizeParam: "limit", CursorParam: "after"}
	query := map[string]string{}

	applyPaginationQuery(query, pagination, map[string]any{"pageSize": float64(10)})

	if _, present := query["after"]; present {
		t.Errorf(`query["after"] = %q, want it omitted entirely (no cursor supplied)`, query["after"])
	}
}

func TestApplyPaginationQuery_LeavesQueryUntouchedWhenTheToolDeclaresNoPagination(t *testing.T) {
	query := map[string]string{"existing": "value"}

	applyPaginationQuery(query, nil, map[string]any{"pageSize": float64(10), "cursor": "abc"})

	if len(query) != 1 || query["existing"] != "value" {
		t.Errorf("query = %+v, want it untouched when pagination is nil", query)
	}
}

// TestApplyPaginationQuery_DoesNotMapAPageSizeArgumentWhenTheParamNameIsNotDeclared
// covers a pagination block that declares only a cursor param (or vice
// versa): an undeclared side must never invent a provider param name.
func TestApplyPaginationQuery_DoesNotMapAnArgumentWhoseProviderParamNameIsNotDeclared(t *testing.T) {
	pagination := &catalog.Pagination{CursorParam: "after"} // no PageSizeParam declared
	query := map[string]string{}

	applyPaginationQuery(query, pagination, map[string]any{"pageSize": float64(10), "cursor": "abc"})

	if len(query) != 1 || query["after"] != "abc" {
		t.Errorf("query = %+v, want only the declared cursor param mapped", query)
	}
}

func TestExtractNextCursor_ReadsTheDottedNextCursorPathOutOfTheDecodedResponse(t *testing.T) {
	pagination := &catalog.Pagination{NextCursorPath: "paging.next.after"}
	data := map[string]any{
		"results": []any{},
		"paging":  map[string]any{"next": map[string]any{"after": "cursor-123"}},
	}

	got := extractNextCursor(pagination, data)

	if got != "cursor-123" {
		t.Errorf("extractNextCursor = %q, want %q", got, "cursor-123")
	}
}

func TestExtractNextCursor_ReturnsEmptyWhenTheResponseCarriesNoFurtherPage(t *testing.T) {
	pagination := &catalog.Pagination{NextCursorPath: "paging.next.after"}
	data := map[string]any{"results": []any{}}

	got := extractNextCursor(pagination, data)

	if got != "" {
		t.Errorf("extractNextCursor = %q, want empty when the response carries no further page", got)
	}
}

func TestExtractNextCursor_ReturnsEmptyWhenPaginationIsNil(t *testing.T) {
	data := map[string]any{"paging": map[string]any{"next": map[string]any{"after": "cursor-123"}}}

	got := extractNextCursor(nil, data)

	if got != "" {
		t.Errorf("extractNextCursor = %q, want empty for a tool with no declared pagination", got)
	}
}

func TestExtractNextCursor_ReturnsEmptyWhenNextCursorPathIsNotDeclared(t *testing.T) {
	pagination := &catalog.Pagination{PageSizeParam: "limit", CursorParam: "after"} // no NextCursorPath
	data := map[string]any{"paging": map[string]any{"next": map[string]any{"after": "cursor-123"}}}

	got := extractNextCursor(pagination, data)

	if got != "" {
		t.Errorf("extractNextCursor = %q, want empty when NextCursorPath is not declared", got)
	}
}

// TestExtractNextCursor_ReturnsEmptyWhenTheResponseShapeDoesNotMatchThePath
// covers a response that isn't the expected nested-object shape at all (e.g.
// a bare string or a differently-shaped object) — extraction must fail safe,
// not panic.
func TestExtractNextCursor_ReturnsEmptyWhenTheResponseShapeDoesNotMatchThePath(t *testing.T) {
	pagination := &catalog.Pagination{NextCursorPath: "paging.next.after"}

	got := extractNextCursor(pagination, "not even an object")

	if got != "" {
		t.Errorf("extractNextCursor = %q, want empty for a response that does not match the declared path's shape", got)
	}
}
