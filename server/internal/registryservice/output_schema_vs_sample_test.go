// Package registryservice (in-package, reusing publish_test.go's
// newFacadeForTest/oneToolBundle harness). Exercises Slice 2's differentiator
// gate (PD63): every tool in a published bundle must declare an output
// schema and a recorded sample response, and the schema must actually
// validate that sample.
package registryservice

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/httpx"
	"beecon/internal/registrybundle"
)

// bundleWithOneCustomTool builds a single-provider bundle carrying exactly
// one tool with caller-supplied output schema and sample, so each gate test
// can shape exactly the failure it wants to provoke.
func bundleWithOneCustomTool(toolSlug string, outputSchema, sample map[string]any) registrybundle.Bundle {
	return registrybundle.Bundle{
		FormatVersion: 1,
		Name:          "Outlook",
		BaseURL:       "https://graph.microsoft.com",
		Tools: []registrybundle.Tool{
			{
				Slug:         toolSlug,
				Name:         "A tool",
				InputSchema:  map[string]any{"type": "object"},
				OutputSchema: outputSchema,
				Sample:       sample,
			},
		},
	}
}

func TestPublish_RejectsAToolWithNoDeclaredOutputSchema(t *testing.T) {
	f := newFacadeForTest()
	bundle := bundleWithOneCustomTool("outlook-list-messages", nil, map[string]any{"status": "ok"})

	_, err := f.Publish(context.Background(), "outlook", bundle)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Publish err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeMissingOutputSchema {
		t.Errorf("Code = %q, want %q", de.Code, CodeMissingOutputSchema)
	}
	if de.Details["tool"] != "outlook-list-messages" {
		t.Errorf("Details[\"tool\"] = %v, want %q", de.Details["tool"], "outlook-list-messages")
	}
}

func TestPublish_RejectsAToolWithNoRecordedSample(t *testing.T) {
	f := newFacadeForTest()
	bundle := bundleWithOneCustomTool("outlook-list-messages", map[string]any{"type": "object"}, nil)

	_, err := f.Publish(context.Background(), "outlook", bundle)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Publish err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeMissingSample {
		t.Errorf("Code = %q, want %q", de.Code, CodeMissingSample)
	}
	if de.Details["tool"] != "outlook-list-messages" {
		t.Errorf("Details[\"tool\"] = %v, want %q", de.Details["tool"], "outlook-list-messages")
	}
}

func TestPublish_RejectsAToolWhoseSampleFailsItsOwnDeclaredOutputSchema(t *testing.T) {
	f := newFacadeForTest()
	outputSchema := map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	}
	sampleMissingRequiredID := map[string]any{"status": "ok"}
	bundle := bundleWithOneCustomTool("outlook-list-messages", outputSchema, sampleMissingRequiredID)

	_, err := f.Publish(context.Background(), "outlook", bundle)

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Publish err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 422 {
		t.Errorf("Status = %d, want 422", de.Status)
	}
	if de.Code != CodeOutputSchemaVsSample {
		t.Errorf("Code = %q, want %q", de.Code, CodeOutputSchemaVsSample)
	}
	if de.Details["tool"] != "outlook-list-messages" {
		t.Errorf("Details[\"tool\"] = %v, want the offending tool's slug %q", de.Details["tool"], "outlook-list-messages")
	}
}

func TestPublish_AcceptsAToolWhoseRecordedSampleValidatesItsDeclaredOutputSchema(t *testing.T) {
	f := newFacadeForTest()
	outputSchema := map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	}
	sampleWithRequiredID := map[string]any{"id": "msg-1"}
	bundle := bundleWithOneCustomTool("outlook-list-messages", outputSchema, sampleWithRequiredID)

	result, err := f.Publish(context.Background(), "outlook", bundle)

	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(result.Tools) != 1 || result.Tools[0].Slug != "outlook-list-messages" {
		t.Errorf("Tools = %+v, want exactly the published tool", result.Tools)
	}
}
