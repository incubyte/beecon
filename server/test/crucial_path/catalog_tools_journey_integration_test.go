//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for
// the package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, initiatedConnectionDTO, wireErrorEnvelope,
// doJSONRequest, outlookDefinitionAgainst, oauthJourneyFixture /
// newOAuthJourneyFixture, openConnectPageAndGetState,
// activateConnectionThroughRealHandshake, executeTool, and
// executionResultDTO — same package). This file tells Slice 1's story end to
// end against the real composition root: the finalized definition format's
// baseUrl + per-tool mapping (list/get/deprecation-filter/pagination over the
// catalog API), a single tool's detail by slug, and — the proof that
// path-parameter templating runs end to end, not just parses — executing
// outlook-get-message against a FakeGraph httptest server whose observed
// request path shows the messageId arrived correctly URL-escaped. A final
// test proves outlook-list-messages still executes under the same
// finalized-format mapping (the back-compat half of AC1 that isn't already
// covered by the Phase 1 journeys, which keep using the old full-URL shape
// unchanged).
package crucial_path

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"beecon/internal/app"
	"beecon/internal/catalog"
	"beecon/test/support"
)

type toolProviderDTO struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Logo string `json:"logo"`
}

type toolSummaryDTO struct {
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  map[string]any  `json:"inputSchema"`
	OutputSchema map[string]any  `json:"outputSchema"`
	Deprecated   bool            `json:"deprecated"`
	Provider     toolProviderDTO `json:"provider"`
}

type toolsPageDTO struct {
	Items      []toolSummaryDTO `json:"items"`
	NextCursor string           `json:"nextCursor"`
}

// outlookDefinitionWithMappingToolsAgainst is outlookDefinitionAgainst
// (oauth_handshake_journey_integration_test.go) re-expressed in the
// finalized definition format's shape: a provider BaseURL plus per-tool
// relative paths and declared query mappings, pointed at fakeGraph instead
// of the real internet — outlook-get-message (path-parameter templating),
// outlook-list-messages (query mapping, proving the finalized format's
// mapping executes the same tool Phase 1 already shipped), and one
// deprecated tool solely to exercise the deprecation filter.
func outlookDefinitionWithMappingToolsAgainst(fakeMS *support.FakeMicrosoft, fakeGraph *support.FakeGraph) []catalog.ProviderDefinition {
	definitions := outlookDefinitionAgainst(fakeMS)
	definitions[0].BaseURL = fakeGraph.BaseURL
	definitions[0].Tools = []catalog.ProviderTool{
		{
			Slug:        "outlook-get-message",
			Name:        "Get email message",
			Description: "Retrieves a specific email message by its ID.",
			Method:      "GET",
			Path:        "/me/messages/{input.messageId}",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"messageId": map[string]any{"type": "string"},
					"select":    map[string]any{"type": "string"},
				},
				"required": []any{"messageId"},
			},
			OutputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":      map[string]any{"type": "string"},
					"subject": map[string]any{"type": "string"},
				},
			},
			Mapping: catalog.Mapping{Query: map[string]string{"$select": "{input.select}"}},
		},
		{
			Slug:        "outlook-list-messages",
			Name:        "List messages",
			Description: "List messages in the authenticated user's mailbox.",
			Method:      "GET",
			Path:        "/me/messages",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"top":    map[string]any{"type": "integer"},
					"skip":   map[string]any{"type": "integer"},
					"select": map[string]any{"type": "string"},
					"filter": map[string]any{"type": "string"},
				},
			},
			OutputSchema: map[string]any{"type": "object"},
			Mapping: catalog.Mapping{Query: map[string]string{
				"top": "{input.top}", "skip": "{input.skip}", "select": "{input.select}", "filter": "{input.filter}",
			}},
		},
		{
			Slug:         "outlook-legacy-tool",
			Name:         "Legacy tool",
			Description:  "A deprecated tool kept only to exercise the deprecation filter.",
			Method:       "GET",
			Path:         "/me/legacy",
			InputSchema:  map[string]any{"type": "object"},
			OutputSchema: map[string]any{"type": "object"},
			Deprecated:   true,
		},
	}
	return definitions
}

func listTools(t *testing.T, wired *app.Wired, orgAuth, query string) (int, toolsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools"+query, orgAuth, "")
	var page toolsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode tools page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func getTool(t *testing.T, wired *app.Wired, orgAuth, slug string) (int, toolSummaryDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/tools/"+slug, orgAuth, "")
	var dto toolSummaryDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode tool detail: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

func TestCatalogToolsJourney_ListPaginationAndDeprecationFilter(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithMappingToolsAgainst(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)

	t.Run("listing tools for the provider excludes deprecated tools by default", func(t *testing.T) {
		status, page := listTools(t, wired, fixture.orgAuth, "?providerSlug=outlook")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 2 {
			t.Fatalf("len(items) = %d, want 2 (deprecated excluded by default)", len(page.Items))
		}
		for _, item := range page.Items {
			if item.Deprecated {
				t.Errorf("item %q is deprecated, want it excluded by default", item.Slug)
			}
		}
	})

	t.Run("includeDeprecated=true includes the deprecated tool with its flag set", func(t *testing.T) {
		status, page := listTools(t, wired, fixture.orgAuth, "?providerSlug=outlook&includeDeprecated=true")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 3 {
			t.Fatalf("len(items) = %d, want 3", len(page.Items))
		}
		found := false
		for _, item := range page.Items {
			if item.Slug == "outlook-legacy-tool" {
				found = true
				if !item.Deprecated {
					t.Error("legacy tool's deprecated flag = false, want true")
				}
			}
		}
		if !found {
			t.Error("outlook-legacy-tool missing from the includeDeprecated=true result")
		}
	})

	t.Run("cursor pagination walks every non-deprecated tool once, sorted by slug, no dupes or gaps", func(t *testing.T) {
		seen := map[string]bool{}
		var order []string
		cursor := ""
		for page := 0; page < 5; page++ {
			status, result := listTools(t, wired, fixture.orgAuth, "?providerSlug=outlook&limit=1&cursor="+cursor)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}
			for _, item := range result.Items {
				if seen[item.Slug] {
					t.Fatalf("slug %q seen more than once while paginating", item.Slug)
				}
				seen[item.Slug] = true
				order = append(order, item.Slug)
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		if len(seen) != 2 {
			t.Fatalf("walked %d tools across all pages, want exactly 2 (no duplicates or gaps)", len(seen))
		}
		for i := 1; i < len(order); i++ {
			if order[i] < order[i-1] {
				t.Errorf("tools out of ascending slug order at index %d: %q came after %q", i, order[i], order[i-1])
			}
		}
	})

	t.Run("listing tools for an unknown integration id returns not-found", func(t *testing.T) {
		status, _ := listTools(t, wired, fixture.orgAuth, "?integrationId=intg_does_not_exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("listing tools for an unknown provider slug returns not-found", func(t *testing.T) {
		status, _ := listTools(t, wired, fixture.orgAuth, "?providerSlug=does-not-exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("fetching a tool by slug returns its detail including input and output schema", func(t *testing.T) {
		status, dto := getTool(t, wired, fixture.orgAuth, "outlook-get-message")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if dto.Slug != "outlook-get-message" {
			t.Errorf("slug = %q, want %q", dto.Slug, "outlook-get-message")
		}
		if dto.Provider.Slug != "outlook" {
			t.Errorf("provider.slug = %q, want %q", dto.Provider.Slug, "outlook")
		}
		if len(dto.InputSchema) == 0 || len(dto.OutputSchema) == 0 {
			t.Errorf("inputSchema/outputSchema must not be empty: %+v / %+v", dto.InputSchema, dto.OutputSchema)
		}
	})

	t.Run("fetching an unknown tool slug returns not-found", func(t *testing.T) {
		status, _ := getTool(t, wired, fixture.orgAuth, "does-not-exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})
}

// TestCatalogToolsJourney_ExecuteOutlookGetMessageEndToEnd is the AC that
// proves path-parameter templating runs end to end, not just parses: a
// messageId containing spaces, a slash, and query-string characters must
// survive the round trip through RenderPath's URL-escaping, over the wire to
// FakeGraph, and back out as the exact same string.
func TestCatalogToolsJourney_ExecuteOutlookGetMessageEndToEnd(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "raw-access-token", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithMappingToolsAgainst(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	const messageID = "message id/needs escaping?&more"

	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-get-message", fixture.userID, initiated.ID,
		fmt.Sprintf(`{"messageId":%q,"select":"subject"}`, messageID))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !result.Successful {
		t.Fatalf("successful = false, want true; error = %+v", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want a decoded JSON object", result.Data)
	}
	if got := data["id"]; got != messageID {
		t.Errorf("data.id = %v, want %q (the templated messageId round-tripped)", got, messageID)
	}

	t.Run("Graph received the messageId as its own path segment, correctly URL-escaped over the wire", func(t *testing.T) {
		if fakeGraph.LastMessageIDPath != messageID {
			t.Errorf("LastMessageIDPath = %q, want %q", fakeGraph.LastMessageIDPath, messageID)
		}
	})

	t.Run("the select input reached Graph as the $select query parameter", func(t *testing.T) {
		query := url.Values(fakeGraph.LastQuery)
		if got := query.Get("$select"); got != "subject" {
			t.Errorf("$select = %q, want %q", got, "subject")
		}
	})
}

// TestCatalogToolsJourney_OutlookListMessagesStillExecutesUnderTheFinalizedMapping
// is AC1's back-compat proof for the finalized (baseUrl + mapping) shape
// itself: outlook-list-messages, re-expressed with a declared query mapping
// instead of Phase 1's generic argument pass-through, still forwards its
// arguments and succeeds exactly as before.
func TestCatalogToolsJourney_OutlookListMessagesStillExecutesUnderTheFinalizedMapping(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: "raw-access-token", RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{})
	wired := support.BootAppWithProviderDefinitions(t, outlookDefinitionWithMappingToolsAgainst(fakeMS, fakeGraph))
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, result := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID,
		`{"top":5,"skip":0,"select":"subject","filter":"isRead eq false"}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !result.Successful {
		t.Fatalf("successful = false, want true; error = %+v", result.Error)
	}
	query := url.Values(fakeGraph.LastQuery)
	if got := query.Get("top"); got != "5" {
		t.Errorf("top = %q, want %q", got, "5")
	}
	if got := query.Get("filter"); got != "isRead eq false" {
		t.Errorf("filter = %q, want %q", got, "isRead eq false")
	}
}
