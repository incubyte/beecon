// Package httpapi_test (same package as registry_service_http_test.go —
// reuses newTestRegistryHTTPServer/testPublishToken). Exercises Slice 2's
// strict-parse gate (PD63) at the HTTP boundary: the Publish handler reads
// the raw request body and rejects an unknown field — top-level or nested —
// before the facade or any of its gates ever run, and asserts the concrete
// status codes the gates behind it produce once parsing succeeds.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doPublishBody(t *testing.T, server *httptest.Server, bearerToken, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/registry/v1/providers/outlook/bundles", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func TestPublish_WithAnUnknownTopLevelFieldReturns422StrictParseFailed(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	body := `{
		"formatVersion": 1,
		"name": "Outlook",
		"tools": [{"slug": "outlook-list-messages", "name": "List messages", "inputSchema": {"type":"object"}, "outputSchema": {"type":"object"}, "sample": {"status":"ok"}}],
		"unexpectedTopLevelField": "oops"
	}`

	resp := doPublishBody(t, server, testPublishToken, body)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusUnprocessableEntity, respBody)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, respBody)
	}
	if envelope.Error.Code != "strict_parse_failed" {
		t.Errorf("error.code = %q, want %q", envelope.Error.Code, "strict_parse_failed")
	}
}

func TestPublish_WithAnUnknownFieldNestedInsideToolMappingReturns422StrictParseFailed(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	body := `{
		"formatVersion": 1,
		"name": "Outlook",
		"tools": [{
			"slug": "outlook-list-messages", "name": "List messages",
			"inputSchema": {"type":"object"}, "outputSchema": {"type":"object"}, "sample": {"status":"ok"},
			"mapping": {"method": "GET", "path": "/messages", "unexpectedMappingField": "oops"}
		}]
	}`

	resp := doPublishBody(t, server, testPublishToken, body)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusUnprocessableEntity, respBody)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, respBody)
	}
	if envelope.Error.Code != "strict_parse_failed" {
		t.Errorf("error.code = %q, want %q", envelope.Error.Code, "strict_parse_failed")
	}
}

func TestPublish_ASampleThatFailsItsOwnOutputSchemaReturns422NamingTheTool(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	body := `{
		"formatVersion": 1,
		"name": "Outlook",
		"tools": [{
			"slug": "outlook-list-messages", "name": "List messages",
			"inputSchema": {"type":"object"},
			"outputSchema": {"type":"object","required":["id"],"properties":{"id":{"type":"string"}}},
			"sample": {"status":"ok"}
		}]
	}`

	resp := doPublishBody(t, server, testPublishToken, body)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusUnprocessableEntity, respBody)
	}
	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, respBody)
	}
	if envelope.Error.Code != "output_schema_vs_sample_mismatch" {
		t.Errorf("error.code = %q, want %q", envelope.Error.Code, "output_schema_vs_sample_mismatch")
	}
	if envelope.Error.Details["tool"] != "outlook-list-messages" {
		t.Errorf("error.details[tool] = %v, want %q", envelope.Error.Details["tool"], "outlook-list-messages")
	}
}

func TestPublish_RepublishingAnAlreadyPublishedVersionReturns409(t *testing.T) {
	server := newTestRegistryHTTPServer(t)
	firstResp := doPublishBody(t, server, testPublishToken, oneToolBundleJSON)
	firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusCreated {
		t.Fatalf("first publish status = %d, want %d", firstResp.StatusCode, http.StatusCreated)
	}

	sameVersionBody := `{
		"formatVersion": 1,
		"name": "Outlook",
		"version": "1.0.0",
		"tools": [{"slug": "outlook-list-messages", "name": "List messages", "inputSchema": {"type":"object"}, "outputSchema": {"type":"object"}, "sample": {"status":"ok"}}]
	}`
	resp := doPublishBody(t, server, testPublishToken, sameVersionBody)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", resp.StatusCode, http.StatusConflict, respBody)
	}
}
