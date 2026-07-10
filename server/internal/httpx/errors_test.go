package httpx_test

import (
	"net/http"
	"testing"

	"beecon/internal/httpx"
)

func TestNew_ConstructsADomainErrorWithTheGivenStatusCodeAndMessage(t *testing.T) {
	err := httpx.New(http.StatusUnprocessableEntity, "validation_failed", "validation failed")

	if err.Status != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", err.Status, http.StatusUnprocessableEntity)
	}
	if err.Code != "validation_failed" {
		t.Errorf("Code = %q, want %q", err.Code, "validation_failed")
	}
	if err.Message != "validation failed" {
		t.Errorf("Message = %q, want %q", err.Message, "validation failed")
	}
}

func TestDomainError_ErrorReturnsTheMessageWhenPresent(t *testing.T) {
	err := httpx.New(http.StatusNotFound, "not_found", "organization not found")

	if got := err.Error(); got != "organization not found" {
		t.Errorf("Error() = %q, want %q", got, "organization not found")
	}
}

func TestDomainError_ErrorFallsBackToCodeWhenMessageIsEmpty(t *testing.T) {
	err := &httpx.DomainError{Status: http.StatusNotFound, Code: "not_found"}

	if got := err.Error(); got != "not_found" {
		t.Errorf("Error() = %q, want %q", got, "not_found")
	}
}

func TestWithDetails_AttachesDetailsWithoutMutatingTheOriginal(t *testing.T) {
	base := httpx.New(http.StatusUnprocessableEntity, "validation_failed", "validation failed")

	withDetails := base.WithDetails(map[string]any{"field": "name"})

	if withDetails.Details["field"] != "name" {
		t.Errorf("Details[field] = %v, want %q", withDetails.Details["field"], "name")
	}
	if base.Details != nil {
		t.Errorf("original DomainError.Details = %v, want nil (WithDetails must not mutate the receiver)", base.Details)
	}
}

func TestUnauthorized_ProducesA401UnauthorizedDomainError(t *testing.T) {
	err := httpx.Unauthorized("missing or invalid admin key")

	if err.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", err.Status, http.StatusUnauthorized)
	}
	if err.Code != "unauthorized" {
		t.Errorf("Code = %q, want %q", err.Code, "unauthorized")
	}
	if err.Message != "missing or invalid admin key" {
		t.Errorf("Message = %q, want %q", err.Message, "missing or invalid admin key")
	}
}
