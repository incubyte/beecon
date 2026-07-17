//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// integrationSummaryDTO, issuedSigningSecretDTO, wireErrorEnvelope,
// doJSONRequest, and mintBrowserUserToken already declared there and in
// browser_token_journey_integration_test.go — same package). This file tells
// Slice 1's story end to end against the real composition root and the real
// embedded outlook.yaml/hubspot.yaml/google-calendar.yaml (support.BootApp,
// no provider-definition override): every shipped trigger
// (outlook-message-received, hubspot-contact-created,
// gcal-event-updated — Phase 5 providers strand's Slice 2, PD80) is visible
// through the catalog API with its real declared schemas, filtered by
// providerSlug or integrationId, cursor-paginated, fetched by slug, rejected
// for an unknown provider/integration, and split between the list endpoint
// (org or user token) and the get-by-slug endpoint (org-key-only) exactly as
// router.go documents.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/test/support"
)

type triggerDefinitionProviderDTO struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Logo string `json:"logo"`
}

type triggerDefinitionSummaryDTO struct {
	Slug          string                       `json:"slug"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	ConfigSchema  map[string]any               `json:"configSchema"`
	PayloadSchema map[string]any               `json:"payloadSchema"`
	Ingestion     string                       `json:"ingestion"`
	Provider      triggerDefinitionProviderDTO `json:"provider"`
}

type triggerDefinitionsPageDTO struct {
	Items      []triggerDefinitionSummaryDTO `json:"items"`
	NextCursor string                        `json:"nextCursor"`
}

// triggerDefinitionsJourneyFixture is the org/integrations/user-token
// scaffolding every sub-test in this file needs: one org key, an Integration
// for each shipped provider (so the integrationId filter has something real
// to resolve), and a valid user-scoped browser token (minted the same way
// browser_token_journey_integration_test.go does, proving AC1's "org or user
// token" list endpoint).
type triggerDefinitionsJourneyFixture struct {
	orgAuth              string
	userAuth             string
	outlookIntegrationID string
	hubspotIntegrationID string
}

func newTriggerDefinitionsJourneyFixture(t *testing.T, wired *app.Wired) triggerDefinitionsJourneyFixture {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var outlookIntegration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"outlook","clientId":"outlook-client-id","clientSecret":"outlook-client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create outlook integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &outlookIntegration); err != nil {
		t.Fatalf("decode outlook integration: %v", err)
	}

	var hubspotIntegration integrationSummaryDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/integrations/", adminAuth,
		`{"providerSlug":"hubspot","clientId":"hubspot-client-id","clientSecret":"hubspot-client-secret"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create hubspot integration status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &hubspotIntegration); err != nil {
		t.Fatalf("decode hubspot integration: %v", err)
	}

	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue org key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode org key: %v", err)
	}
	orgAuth := "Bearer " + orgKey.Key

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	var signingSecret issuedSigningSecretDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/signing-secrets", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue signing secret status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &signingSecret); err != nil {
		t.Fatalf("decode signing secret: %v", err)
	}

	now := time.Now()
	userToken := mintBrowserUserToken(t, signingSecret.Secret, signingSecret.ID, "HS256", user.ID, now.Unix(), now.Add(2*time.Hour).Unix())

	return triggerDefinitionsJourneyFixture{
		orgAuth:              orgAuth,
		userAuth:             "Bearer " + userToken,
		outlookIntegrationID: outlookIntegration.ID,
		hubspotIntegrationID: hubspotIntegration.ID,
	}
}

func listTriggerDefinitions(t *testing.T, wired *app.Wired, auth, query string) (int, triggerDefinitionsPageDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/trigger-definitions"+query, auth, "")
	var page triggerDefinitionsPageDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode trigger definitions page: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, page
}

func getTriggerDefinition(t *testing.T, wired *app.Wired, auth, slug string) (int, triggerDefinitionSummaryDTO) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/trigger-definitions/"+slug, auth, "")
	var dto triggerDefinitionSummaryDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode trigger definition detail: %v; body=%s", err, w.Body.String())
		}
	}
	return w.Code, dto
}

// TestTriggerDefinitionsJourney_ListFilterGetAndPagination is Slice 1's ACs 1,
// 2, 3, and 6 end to end against the real embedded provider definitions.
func TestTriggerDefinitionsJourney_ListFilterGetAndPagination(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newTriggerDefinitionsJourneyFixture(t, wired)

	t.Run("listing by providerSlug=outlook returns outlook-message-received with its real folderId config default and payload schema", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, fixture.orgAuth, "?providerSlug=outlook")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(page.Items))
		}
		item := page.Items[0]
		if item.Slug != "outlook-message-received" {
			t.Errorf("slug = %q, want %q", item.Slug, "outlook-message-received")
		}
		if item.Ingestion != "poll" {
			t.Errorf("ingestion = %q, want %q", item.Ingestion, "poll")
		}
		if item.Provider.Slug != "outlook" || item.Provider.Name != "Outlook" || item.Provider.Logo == "" {
			t.Errorf("provider = %+v, want outlook/Outlook/<logo>", item.Provider)
		}
		folderIDProp, ok := propertyOf(t, item.ConfigSchema, "folderId")
		if !ok {
			t.Fatalf("configSchema %+v has no folderId property", item.ConfigSchema)
		}
		if folderIDProp["default"] != "Inbox" {
			t.Errorf("configSchema.properties.folderId.default = %v, want %q", folderIDProp["default"], "Inbox")
		}
		for _, field := range []string{"id", "subject", "from", "receivedDateTime", "bodyPreview", "folderId"} {
			if _, ok := propertyOf(t, item.PayloadSchema, field); !ok {
				t.Errorf("payloadSchema %+v is missing property %q", item.PayloadSchema, field)
			}
		}
	})

	t.Run("listing by integrationId resolves to that integration's provider's trigger", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, fixture.orgAuth, "?integrationId="+fixture.hubspotIntegrationID)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 1 || page.Items[0].Slug != "hubspot-contact-created" {
			t.Fatalf("items = %+v, want exactly hubspot-contact-created", page.Items)
		}
	})

	t.Run("listing with no filter returns both shipped triggers with correct schemas", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, fixture.orgAuth, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		bySlug := map[string]triggerDefinitionSummaryDTO{}
		for _, item := range page.Items {
			bySlug[item.Slug] = item
		}
		outlookTrigger, ok := bySlug["outlook-message-received"]
		if !ok {
			t.Fatalf("items %+v missing outlook-message-received", page.Items)
		}
		if outlookTrigger.Provider.Slug != "outlook" {
			t.Errorf("outlook trigger provider.slug = %q, want %q", outlookTrigger.Provider.Slug, "outlook")
		}
		hubspotTrigger, ok := bySlug["hubspot-contact-created"]
		if !ok {
			t.Fatalf("items %+v missing hubspot-contact-created", page.Items)
		}
		if hubspotTrigger.Provider.Slug != "hubspot" {
			t.Errorf("hubspot trigger provider.slug = %q, want %q", hubspotTrigger.Provider.Slug, "hubspot")
		}
		for _, field := range []string{"id", "properties"} {
			if _, ok := propertyOf(t, hubspotTrigger.PayloadSchema, field); !ok {
				t.Errorf("hubspot payloadSchema %+v is missing property %q", hubspotTrigger.PayloadSchema, field)
			}
		}
	})

	t.Run("cursor pagination walks every shipped trigger once, sorted by slug, no dupes or gaps", func(t *testing.T) {
		seen := map[string]bool{}
		cursor := ""
		for page := 0; page < 5; page++ {
			status, result := listTriggerDefinitions(t, wired, fixture.orgAuth, "?limit=1&cursor="+cursor)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want %d", status, http.StatusOK)
			}
			for _, item := range result.Items {
				if seen[item.Slug] {
					t.Fatalf("slug %q seen more than once while paginating", item.Slug)
				}
				seen[item.Slug] = true
			}
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}
		if len(seen) != 3 {
			t.Fatalf("walked %d triggers across all pages, want exactly 3 (no duplicates or gaps): %v", len(seen), seen)
		}
	})

	t.Run("fetching outlook-message-received by slug returns its full detail", func(t *testing.T) {
		status, dto := getTriggerDefinition(t, wired, fixture.orgAuth, "outlook-message-received")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if dto.Slug != "outlook-message-received" {
			t.Errorf("slug = %q, want %q", dto.Slug, "outlook-message-received")
		}
		if len(dto.ConfigSchema) == 0 || len(dto.PayloadSchema) == 0 {
			t.Errorf("configSchema/payloadSchema must not be empty: %+v / %+v", dto.ConfigSchema, dto.PayloadSchema)
		}
	})

	t.Run("listing for an unknown provider slug returns not-found", func(t *testing.T) {
		status, _ := listTriggerDefinitions(t, wired, fixture.orgAuth, "?providerSlug=does-not-exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("listing for an unknown integrationId returns not-found", func(t *testing.T) {
		status, _ := listTriggerDefinitions(t, wired, fixture.orgAuth, "?integrationId=intg_does_not_exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})

	t.Run("fetching an unknown trigger slug returns not-found", func(t *testing.T) {
		status, _ := getTriggerDefinition(t, wired, fixture.orgAuth, "does-not-exist")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", status, http.StatusNotFound)
		}
	})
}

// TestTriggerDefinitionsJourney_ListAcceptsAUserTokenButGetIsOrgKeyOnly is the
// API Shape's documented split (also router.go's own comment): list is
// mounted under orgOrUser, get-by-slug stays org-key-only, mirroring /tools.
func TestTriggerDefinitionsJourney_ListAcceptsAUserTokenButGetIsOrgKeyOnly(t *testing.T) {
	wired := support.BootApp(t)
	fixture := newTriggerDefinitionsJourneyFixture(t, wired)

	t.Run("a valid user token can list trigger definitions", func(t *testing.T) {
		status, page := listTriggerDefinitions(t, wired, fixture.userAuth, "?providerSlug=outlook")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if len(page.Items) != 1 {
			t.Fatalf("len(items) = %d, want 1", len(page.Items))
		}
	})

	t.Run("a valid user token cannot fetch a trigger definition by slug", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/trigger-definitions/outlook-message-received", fixture.userAuth, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})
}

// propertyOf reads schema.properties[name] as a map[string]any, the shape
// every JSON-Schema property in this file's fixtures takes.
func propertyOf(t *testing.T, schema map[string]any, name string) (map[string]any, bool) {
	t.Helper()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	prop, ok := properties[name].(map[string]any)
	return prop, ok
}
