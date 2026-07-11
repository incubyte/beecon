// Package catalog_test exercises catalog.Facade against the in-memory
// Repository, with a fake ProviderDefinition set so tests do not depend on
// the real embedded outlook.yaml (definition_test.go and embed_test-style
// coverage already exercises that file directly).
package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"beecon/internal/catalog"
	memory "beecon/internal/catalog/driven/memory"
	"beecon/internal/httpx"
)

func fakeDefinitions() []catalog.ProviderDefinition {
	return []catalog.ProviderDefinition{
		{
			Slug:         "outlook",
			Name:         "Outlook",
			Logo:         "https://static.beecon.dev/providers/outlook.png",
			AuthScheme:   "oauth2",
			AuthorizeURL: "https://example.com/authorize",
			TokenURL:     "https://example.com/token",
			Scopes:       []string{"Mail.Read"},
		},
	}
}

func newCatalogFacade(t *testing.T) *catalog.Facade {
	t.Helper()
	f, err := memory.NewFacadeWithOverrides(memory.Overrides{Definitions: fakeDefinitions()})
	if err != nil {
		t.Fatalf("NewFacadeWithOverrides: %v", err)
	}
	return f
}

func assertDomainError(t *testing.T, err error, wantCode string, wantStatus int) *httpx.DomainError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected domain error with code %q, got nil", wantCode)
	}
	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *httpx.DomainError, got %T: %v", err, err)
	}
	if de.Code != wantCode {
		t.Fatalf("error code = %q, want %q", de.Code, wantCode)
	}
	if de.Status != wantStatus {
		t.Fatalf("error status = %d, want %d", de.Status, wantStatus)
	}
	return de
}

func TestCreateIntegration_MintsAnIntgPrefixedIDDeterministically(t *testing.T) {
	f := newCatalogFacade(t)

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(summary.ID) != "intg_1" {
		t.Errorf("ID = %q, want %q (deterministic sequential id from the memory fake)", summary.ID, "intg_1")
	}
}

func TestCreateIntegration_RejectsAnUnknownProviderSlug(t *testing.T) {
	f := newCatalogFacade(t)

	_, err := f.CreateIntegration(context.Background(), "does-not-exist", "client-id", "client-secret")

	assertDomainError(t, err, catalog.CodeValidationFailed, 422)
}

func TestCreateIntegration_SummaryCarriesProviderNameLogoAndAuthSchemeFromTheDefinition(t *testing.T) {
	f := newCatalogFacade(t)

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.ProviderSlug != "outlook" {
		t.Errorf("ProviderSlug = %q, want %q", summary.ProviderSlug, "outlook")
	}
	if summary.ProviderName != "Outlook" {
		t.Errorf("ProviderName = %q, want %q", summary.ProviderName, "Outlook")
	}
	if summary.Logo != "https://static.beecon.dev/providers/outlook.png" {
		t.Errorf("Logo = %q, want the provider definition's logo", summary.Logo)
	}
	if summary.AuthScheme != "oauth2" {
		t.Errorf("AuthScheme = %q, want %q", summary.AuthScheme, "oauth2")
	}
}

// TestCreateIntegration_SummaryNeverSerializesTheClientSecret is a
// belt-and-suspenders behavior test for AC4: even though IntegrationSummary
// carries no ClientSecret field at the type level, this test guards against a
// future field addition silently leaking the secret by asserting on the
// actual JSON bytes a caller would see.
func TestCreateIntegration_SummaryNeverSerializesTheClientSecret(t *testing.T) {
	f := newCatalogFacade(t)
	const secret = "super-secret-oauth-client-secret"

	summary, err := f.CreateIntegration(context.Background(), "outlook", "client-id", secret)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("IntegrationSummary JSON %s contains the client secret — AC4 requires it never appear in any API response after creation", encoded)
	}
}

func TestListIntegrations_ReturnsEverySummaryOrderedByCreation(t *testing.T) {
	f := newCatalogFacade(t)
	ctx := context.Background()
	first, err := f.CreateIntegration(ctx, "outlook", "client-1", "secret-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.CreateIntegration(ctx, "outlook", "client-2", "secret-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	summaries, err := f.ListIntegrations(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2", len(summaries))
	}
	if summaries[0].ID != first.ID || summaries[1].ID != second.ID {
		t.Errorf("order = [%q, %q], want creation order [%q, %q]", summaries[0].ID, summaries[1].ID, first.ID, second.ID)
	}
}

func TestListIntegrations_ReturnsEmptySliceWhenNoneExist(t *testing.T) {
	f := newCatalogFacade(t)

	summaries, err := f.ListIntegrations(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("summaries = %+v, want empty", summaries)
	}
}

func TestGetIntegration_ReturnsAPreviouslyCreatedIntegrationIncludingItsClientSecret(t *testing.T) {
	f := newCatalogFacade(t)
	created, err := f.CreateIntegration(context.Background(), "outlook", "client-id", "client-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.GetIntegration(context.Background(), created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientSecret != "client-secret" {
		t.Errorf("ClientSecret = %q, want %q (internal reads need the secret to run OAuth; only the summary hides it)", got.ClientSecret, "client-secret")
	}
	if !reflect.DeepEqual(got.ID, created.ID) {
		t.Errorf("ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetIntegration_ReturnsTypedNotFoundForAnUnknownID(t *testing.T) {
	f := newCatalogFacade(t)

	_, err := f.GetIntegration(context.Background(), catalog.IntegrationID("intg_missing"))

	assertDomainError(t, err, catalog.CodeNotFound, 404)
}
