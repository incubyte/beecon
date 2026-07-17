package membraneimport

import (
	"strings"
	"testing"
)

// findProviderBySlug locates one ProviderOutput by slug — used by tests that
// run Convert over more than one group and want to assert on one group's
// output without depending on emission order.
func findProviderBySlug(providers []ProviderOutput, slug string) (ProviderOutput, bool) {
	for _, p := range providers {
		if p.Slug == slug {
			return p, true
		}
	}
	return ProviderOutput{}, false
}

// billingIntegrationAndAction returns a second, independent integration+action
// pair sharing their own integrationUuid, distinct from the testdata/
// fixtures' "grp-test-crm-uuid" group — used to prove Convert keeps separate
// groups separate rather than merging everything it sees into one provider.
func billingIntegrationAndAction() []SourceFile {
	integration := SourceFile{Name: "billing-integration.yaml", Content: []byte(`
uuid: grp-test-billing-uuid
key: test-billing
name: Test Billing
logoUri: https://static.example.com/test-billing.png
`)}
	action := SourceFile{Name: "billing-action.yaml", Content: []byte(`
key: test-billing-list-invoices
name: List Invoices
inputSchema:
  description: Lists invoices.
  type: object
  properties: {}
type: api-request-to-external-app
config:
  request:
    method: GET
    path: /invoices
customOutputSchema:
  type: object
  properties:
    id:
      type: string
integrationUuid: grp-test-billing-uuid
`)}
	return []SourceFile{integration, action}
}

// TestConvert_DerivesProviderIdentityFromTheIntegrationRecord verifies the
// emitted provider's slug/name/logo trace back to the Membrane integration
// record's own key/name/logoUri fields (Slice 1 AC: slug is lower-kebab and
// non-empty; name/logo copied through).
func TestConvert_DerivesProviderIdentityFromTheIntegrationRecord(t *testing.T) {
	files := []SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(result.Providers))
	}

	if got := result.Providers[0].Slug; got != "test-crm" {
		t.Errorf("Slug = %q, want %q (lower-kebab of integration key %q)", got, "test-crm", "test-crm")
	}
	body := string(result.Providers[0].YAML)
	if !strings.Contains(body, "name: Test CRM") {
		t.Errorf("emitted YAML does not carry the integration record's name:\n%s", body)
	}
	if !strings.Contains(body, "logo: https://static.example.com/test-crm.png") {
		t.Errorf("emitted YAML does not carry the integration record's logoUri as logo:\n%s", body)
	}
}

// TestConvert_EmitsOneProviderPerSharedIntegrationUuid is the grouping AC:
// two integration+action pairs carrying two different integrationUuid values
// must produce two separate provider files, each with exactly its own tool —
// Convert must not merge unrelated groups nor drop either one.
func TestConvert_EmitsOneProviderPerSharedIntegrationUuid(t *testing.T) {
	files := append([]SourceFile{
		loadTestdataFile(t, "integration.yaml"),
		loadTestdataFile(t, "action-get-record.yaml"),
	}, billingIntegrationAndAction()...)

	result, err := Convert(files)
	if err != nil {
		t.Fatalf("Convert returned an error: %v", err)
	}
	if len(result.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2 (one per distinct integrationUuid)", len(result.Providers))
	}

	crm, ok := findProviderBySlug(result.Providers, "test-crm")
	if !ok {
		t.Fatalf("Providers = %+v, want it to include slug %q", result.Providers, "test-crm")
	}
	if !strings.Contains(string(crm.YAML), "test-crm-get-record") {
		t.Errorf("test-crm provider YAML missing its own tool:\n%s", crm.YAML)
	}

	billing, ok := findProviderBySlug(result.Providers, "test-billing")
	if !ok {
		t.Fatalf("Providers = %+v, want it to include slug %q", result.Providers, "test-billing")
	}
	if !strings.Contains(string(billing.YAML), "test-billing-list-invoices") {
		t.Errorf("test-billing provider YAML missing its own tool:\n%s", billing.YAML)
	}
	if strings.Contains(string(billing.YAML), "test-crm-get-record") {
		t.Errorf("test-billing provider YAML leaked the other group's tool:\n%s", billing.YAML)
	}
}
