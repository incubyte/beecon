package registryservice

import (
	"context"
	"errors"
	"testing"

	"beecon/internal/httpx"
)

func TestPull_ReturnsThePreviouslyPublishedBundleIncludingEveryToolsID(t *testing.T) {
	f := newFacadeForTest()
	published, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages"))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	pulled, err := f.Pull(context.Background(), "outlook", published.Version)

	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if pulled.ProviderSlug != "outlook" {
		t.Errorf("ProviderSlug = %q, want %q", pulled.ProviderSlug, "outlook")
	}
	if pulled.Version != published.Version {
		t.Errorf("Version = %q, want %q", pulled.Version, published.Version)
	}
	if len(pulled.Tools) != 1 || pulled.Tools[0].ID != published.Tools[0].ID {
		t.Errorf("Tools = %+v, want the tool carrying id %q", pulled.Tools, published.Tools[0].ID)
	}
	if pulled.Tools[0].InputSchema == nil || pulled.Tools[0].OutputSchema == nil {
		t.Errorf("pulled tool's input/output schemas must be present: %+v", pulled.Tools[0])
	}
}

func TestPull_UnknownVersionReturnsNotFound(t *testing.T) {
	f := newFacadeForTest()
	if _, err := f.Publish(context.Background(), "outlook", oneToolBundle("outlook-list-messages")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	_, err := f.Pull(context.Background(), "outlook", "9.9.9")

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Pull err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 404 {
		t.Errorf("Status = %d, want 404", de.Status)
	}
	if de.Code != CodeNotFound {
		t.Errorf("Code = %q, want %q", de.Code, CodeNotFound)
	}
}

func TestPull_UnknownProviderReturnsNotFound(t *testing.T) {
	f := newFacadeForTest()

	_, err := f.Pull(context.Background(), "never-published", "1.0.0")

	var de *httpx.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("Pull err = %T, want *httpx.DomainError", err)
	}
	if de.Status != 404 {
		t.Errorf("Status = %d, want 404", de.Status)
	}
}
