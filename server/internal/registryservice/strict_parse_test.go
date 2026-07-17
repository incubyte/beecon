// Package registryservice (in-package, matching publish_test.go/pull_test.go's
// convention). Exercises ParseBundleStrict directly: Slice 2's strict-parse
// gate (PD63) applied to the raw JSON a publish request carries, ahead of
// everything else Publish does.
package registryservice

import (
	"errors"
	"testing"

	"beecon/internal/httpx"
)

const wellFormedBundleJSON = `{
	"formatVersion": 1,
	"name": "Outlook",
	"baseUrl": "https://graph.microsoft.com",
	"tools": [
		{
			"slug": "outlook-list-messages",
			"name": "List messages",
			"inputSchema": {"type": "object"},
			"outputSchema": {"type": "object"},
			"sample": {"status": "ok"},
			"mapping": {"method": "GET", "path": "/messages"}
		}
	]
}`

func TestParseBundleStrict_RejectsAnUnknownTopLevelField(t *testing.T) {
	raw := []byte(`{
		"formatVersion": 1,
		"name": "Outlook",
		"baseUrl": "https://graph.microsoft.com",
		"tools": [],
		"unexpectedTopLevelField": "oops"
	}`)

	_, err := ParseBundleStrict(raw)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("ParseBundleStrict err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeStrictParseFailed {
		t.Errorf("Code = %q, want %q", de.Code, CodeStrictParseFailed)
	}
}

func TestParseBundleStrict_RejectsAnUnknownFieldNestedInsideToolMapping(t *testing.T) {
	raw := []byte(`{
		"formatVersion": 1,
		"name": "Outlook",
		"baseUrl": "https://graph.microsoft.com",
		"tools": [
			{
				"slug": "outlook-list-messages",
				"name": "List messages",
				"inputSchema": {"type": "object"},
				"outputSchema": {"type": "object"},
				"sample": {"status": "ok"},
				"mapping": {"method": "GET", "path": "/messages", "unexpectedMappingField": "oops"}
			}
		]
	}`)

	_, err := ParseBundleStrict(raw)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("ParseBundleStrict err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeStrictParseFailed {
		t.Errorf("Code = %q, want %q", de.Code, CodeStrictParseFailed)
	}
}

func TestParseBundleStrict_AcceptsAWellFormedBundle(t *testing.T) {
	bundle, err := ParseBundleStrict([]byte(wellFormedBundleJSON))

	if err != nil {
		t.Fatalf("ParseBundleStrict: %v", err)
	}
	if bundle.Name != "Outlook" {
		t.Errorf("Name = %q, want %q", bundle.Name, "Outlook")
	}
	if len(bundle.Tools) != 1 || bundle.Tools[0].Slug != "outlook-list-messages" {
		t.Errorf("Tools = %+v, want exactly one tool slugged %q", bundle.Tools, "outlook-list-messages")
	}
}
