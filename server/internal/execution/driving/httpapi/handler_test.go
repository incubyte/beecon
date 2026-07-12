// Package httpapi (in-package test) exercises the execution module's HTTP
// handler through an actual chi router mounted with the same route pattern
// app/router.go uses, backed by hand-written fakes for the facade's narrow
// ports (mirroring connections/driving/httpapi/handler_test.go's pattern).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/execution"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

const (
	testOrg          = organizations.OrgID("org_1")
	otherOrg         = organizations.OrgID("org_2")
	testUser         = organizations.UserID("user_1")
	testConnectionID = connections.ConnectionID("conn_1")
	testToolSlug     = "outlook-list-messages"
)

type fakeToolReader struct{}

func (fakeToolReader) FindToolBySlug(_ context.Context, slug string) (catalog.ProviderDefinition, catalog.ProviderTool, error) {
	if slug != testToolSlug {
		return catalog.ProviderDefinition{}, catalog.ProviderTool{}, catalog.ErrToolNotFound()
	}
	return catalog.ProviderDefinition{Slug: "outlook"}, catalog.ProviderTool{
		Slug:   testToolSlug,
		Method: "GET",
		Path:   "https://graph.microsoft.com/v1.0/me/messages",
	}, nil
}

type fakeConnectionReader struct{}

func (fakeConnectionReader) ResolveForExecution(_ context.Context, org organizations.OrgID, _ organizations.UserID, id connections.ConnectionID) (connections.ExecutionAccess, error) {
	if org != testOrg || id != testConnectionID {
		return connections.ExecutionAccess{}, connections.ErrNotFound()
	}
	return connections.ExecutionAccess{Status: connections.StatusActive, AccessToken: "the-access-token"}, nil
}

type fakeProviderClient struct{}

func (fakeProviderClient) Call(_ context.Context, _ execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	return execution.ToolCallResponse{StatusCode: 200, Body: `{"value":[{"id":"msg-1"}]}`}, nil
}

func newTestRouter() chi.Router {
	facade := execution.NewFacade(fakeToolReader{}, fakeConnectionReader{}, fakeProviderClient{}, nil, func() time.Time { return time.Now() })
	errorRenderer := httpx.NewErrorRenderer(nil)
	h := NewHandler(facade, errorRenderer)

	r := chi.NewRouter()
	r.Post("/{slug}/execute", h.Execute)
	return r
}

func doRequestAsOrg(r chi.Router, method, path string, org organizations.OrgID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func executeBody(userID, connectionID string, arguments string) string {
	return `{"userId":"` + userID + `","connectionId":"` + connectionID + `","arguments":` + arguments + `}`
}

func TestExecute_Returns200WithSuccessfulResultForAValidCall(t *testing.T) {
	r := newTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/"+testToolSlug+"/execute", testOrg,
		executeBody(string(testUser), string(testConnectionID), `{}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto executionResultDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if !dto.Successful {
		t.Errorf("successful = false, want true")
	}
	if dto.Error != nil {
		t.Errorf("error = %+v, want nil", dto.Error)
	}
}

func TestExecute_Returns401WhenNoOrgContext(t *testing.T) {
	r := newTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/"+testToolSlug+"/execute", "",
		executeBody(string(testUser), string(testConnectionID), `{}`))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestExecute_Returns422ForAMalformedJSONBody(t *testing.T) {
	r := newTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/"+testToolSlug+"/execute", testOrg, `{"userId":`)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestExecute_Returns404ForAnUnknownToolSlug(t *testing.T) {
	r := newTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/unknown-slug/execute", testOrg,
		executeBody(string(testUser), string(testConnectionID), `{}`))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestExecute_Returns404ForAConnectionOfAnotherOrganization(t *testing.T) {
	r := newTestRouter()

	w := doRequestAsOrg(r, http.MethodPost, "/"+testToolSlug+"/execute", otherOrg,
		executeBody(string(testUser), string(testConnectionID), `{}`))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestExecute_Returns200WithFailureResultForInvalidArguments(t *testing.T) {
	facade := execution.NewFacade(fakeToolReaderWithSchema{}, fakeConnectionReader{}, fakeProviderClient{}, nil, func() time.Time { return time.Now() })
	errorRenderer := httpx.NewErrorRenderer(nil)
	h := NewHandler(facade, errorRenderer)
	r := chi.NewRouter()
	r.Post("/{slug}/execute", h.Execute)

	w := doRequestAsOrg(r, http.MethodPost, "/"+testToolSlug+"/execute", testOrg,
		executeBody(string(testUser), string(testConnectionID), `{"top":"not-a-number"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (AC2: invalid arguments is a tool-level failure, not an HTTP error); body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var dto executionResultDTO
	if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
	}
	if dto.Successful {
		t.Error("successful = true, want false for invalid arguments")
	}
	if dto.Error == nil || dto.Error.Code != execution.CodeInvalidArguments {
		t.Errorf("error = %+v, want code %q", dto.Error, execution.CodeInvalidArguments)
	}
	if dto.Data != nil {
		t.Errorf("data = %v, want nil", dto.Data)
	}
}

// fakeToolReaderWithSchema is a variant of fakeToolReader whose tool declares
// an input schema, so the invalid-arguments test above can exercise AC2 at
// the HTTP layer.
type fakeToolReaderWithSchema struct{}

func (fakeToolReaderWithSchema) FindToolBySlug(_ context.Context, slug string) (catalog.ProviderDefinition, catalog.ProviderTool, error) {
	if slug != testToolSlug {
		return catalog.ProviderDefinition{}, catalog.ProviderTool{}, catalog.ErrToolNotFound()
	}
	return catalog.ProviderDefinition{Slug: "outlook"}, catalog.ProviderTool{
		Slug:   testToolSlug,
		Method: "GET",
		Path:   "https://graph.microsoft.com/v1.0/me/messages",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"top": map[string]any{"type": "integer"}},
		},
	}, nil
}
