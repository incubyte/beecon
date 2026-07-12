// Package providerhttp_test exercises Client.Call against a real
// httptest.Server: the one behavioral gap facade_test.go's fakes cannot
// close, since ProviderClient is faked there — this proves ToolCallRequest's
// declared header mapping (PD13) actually lands on the wire as an HTTP
// header, not just inside an in-memory struct field.
package providerhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/execution"
	"beecon/internal/execution/driven/providerhttp"
)

func TestCall_ForwardsDeclaredHeadersOnTheActualHTTPRequest(t *testing.T) {
	var receivedPrefer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPrefer = r.Header.Get("Prefer")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{
		Method:      http.MethodGet,
		URL:         server.URL,
		AccessToken: "token-value",
		Headers:     map[string]string{"Prefer": "return=minimal"},
	}

	_, err := client.Call(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPrefer != "return=minimal" {
		t.Errorf("received Prefer header = %q, want %q", receivedPrefer, "return=minimal")
	}
}

func TestCall_SendsNoExtraHeaderWhenNoneAreDeclared(t *testing.T) {
	var receivedPrefer string
	sawHeader := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPrefer, sawHeader = r.Header.Get("Prefer"), r.Header.Get("Prefer") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{Method: http.MethodGet, URL: server.URL, AccessToken: "token-value"}

	_, err := client.Call(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawHeader {
		t.Errorf("received Prefer header = %q, want no Prefer header sent at all", receivedPrefer)
	}
}
