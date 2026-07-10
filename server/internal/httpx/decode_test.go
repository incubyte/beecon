package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/httpx"
)

type decodeTarget struct {
	Name string `json:"name"`
}

func TestDecodeJSON_PopulatesTheDestinationFromAWellFormedBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Acme"}`))

	var dst decodeTarget
	if err := httpx.DecodeJSON(r, &dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst.Name != "Acme" {
		t.Errorf("Name = %q, want %q", dst.Name, "Acme")
	}
}

func TestDecodeJSON_EmptyBodyLeavesTheDestinationAtItsZeroValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)

	var dst decodeTarget
	if err := httpx.DecodeJSON(r, &dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst.Name != "" {
		t.Errorf("Name = %q, want empty string for an empty body", dst.Name)
	}
}

func TestDecodeJSON_MalformedBodyReturnsAnError(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))

	var dst decodeTarget
	err := httpx.DecodeJSON(r, &dst)

	if err == nil {
		t.Fatal("expected an error for malformed JSON, got nil")
	}
}
