package delivery

import (
	"errors"
	"testing"

	"beecon/internal/httpx"
)

// TestValidateEndpointURL_AcceptsAbsoluteHTTPAndHTTPSURLs covers PD31's own
// AC: the URL must be an absolute http(s) URL — nothing more restrictive.
func TestValidateEndpointURL_AcceptsAbsoluteHTTPAndHTTPSURLs(t *testing.T) {
	valid := []string{
		"http://example.com/webhook",
		"https://example.com/webhook",
		"https://example.com/webhook?with=query&params=1",
		"https://example.com:8443/deep/path/webhook",
		"http://sub.domain.example.com/webhook",
	}
	for _, url := range valid {
		if err := ValidateEndpointURL(url); err != nil {
			t.Errorf("ValidateEndpointURL(%q) = %v, want nil (a valid absolute http(s) URL)", url, err)
		}
	}
}

// TestValidateEndpointURL_DeliberatelyAcceptsPrivateAndLoopbackAddresses
// pins the documented operator-note decision (types.go's own comment, PD31):
// org-key holders are trusted operators of their own self-hosted
// installation, so private/loopback addresses are not blocked here — this
// test exists specifically so a future "helpful" SSRF-style lockdown doesn't
// get added silently, breaking self-hosted operators who legitimately point
// their endpoint at an internal address.
func TestValidateEndpointURL_DeliberatelyAcceptsPrivateAndLoopbackAddresses(t *testing.T) {
	trusted := []string{
		"http://127.0.0.1:8080/webhook",
		"http://localhost/webhook",
		"http://10.0.0.5/webhook",
		"http://192.168.1.50:9000/webhook",
	}
	for _, url := range trusted {
		if err := ValidateEndpointURL(url); err != nil {
			t.Errorf("ValidateEndpointURL(%q) = %v, want nil (private/loopback addresses are deliberately allowed, PD31)", url, err)
		}
	}
}

func TestValidateEndpointURL_RejectsANonAbsoluteOrNonHTTPValue(t *testing.T) {
	invalid := []string{
		"",
		"not-a-url at all",
		"/relative/path",
		"example.com/webhook",
		"ftp://example.com/webhook",
		"ws://example.com/webhook",
		"http://",
		"https://",
		"mailto:someone@example.com",
	}
	for _, url := range invalid {
		err := ValidateEndpointURL(url)
		if err == nil {
			t.Errorf("ValidateEndpointURL(%q) = nil, want a validation error", url)
			continue
		}
		var domainErr *httpx.DomainError
		if !errors.As(err, &domainErr) {
			t.Errorf("ValidateEndpointURL(%q) error = %T, want *httpx.DomainError", url, err)
			continue
		}
		if domainErr.Code != CodeValidationFailed {
			t.Errorf("ValidateEndpointURL(%q) error code = %q, want %q", url, domainErr.Code, CodeValidationFailed)
		}
	}
}
