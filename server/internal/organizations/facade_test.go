// Package organizations_test must be external (not in-package): driven/memory
// imports organizations, so an in-package test importing it would be an
// import cycle. Domain errors are *httpx.DomainError, so assertions use
// errors.As to check Code/Status/Details rather than errors.Is against a
// singleton.
package organizations_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/organizations"
	memory "beecon/internal/organizations/driven/memory"
)

// newFacade builds an organizations facade over the in-memory defaults:
// deterministic "org_1", "org_2", … ids and a fixed clock.
func newFacade() *organizations.Facade {
	return memory.NewFacadeWithOverrides(memory.Overrides{})
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

func TestCreate_MintsAnOrgPrefixedID(t *testing.T) {
	f := newFacade()

	org, err := f.Create(context.Background(), "Acme")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(org.ID) != "org_1" {
		t.Errorf("ID = %q, want %q (deterministic sequential id from the memory fake)", org.ID, "org_1")
	}
}

func TestCreate_IDsAreSequentialAcrossMultipleCreates(t *testing.T) {
	f := newFacade()

	first, err := f.Create(context.Background(), "First")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.Create(context.Background(), "Second")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(first.ID) != "org_1" {
		t.Errorf("first.ID = %q, want %q", first.ID, "org_1")
	}
	if string(second.ID) != "org_2" {
		t.Errorf("second.ID = %q, want %q", second.ID, "org_2")
	}
}

func TestCreate_StoresTheSuppliedNameAndTimestampFromTheFixedClock(t *testing.T) {
	f := newFacade()

	org, err := f.Create(context.Background(), "Acme")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org.Name != "Acme" {
		t.Errorf("Name = %q, want %q", org.Name, "Acme")
	}
	wantCreatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !org.CreatedAt.Equal(wantCreatedAt) {
		t.Errorf("CreatedAt = %v, want %v (the memory fake's fixed clock)", org.CreatedAt, wantCreatedAt)
	}
}

func TestCreate_TrimsSurroundingWhitespaceFromTheName(t *testing.T) {
	f := newFacade()

	org, err := f.Create(context.Background(), "  Acme  ")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if org.Name != "Acme" {
		t.Errorf("Name = %q, want trimmed %q", org.Name, "Acme")
	}
}

func TestCreate_RejectsAnEmptyName(t *testing.T) {
	f := newFacade()

	_, err := f.Create(context.Background(), "")

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)
}

func TestCreate_RejectsAWhitespaceOnlyName(t *testing.T) {
	f := newFacade()

	_, err := f.Create(context.Background(), "   ")

	de := assertDomainError(t, err, organizations.CodeValidationFailed, 422)
	if de.Details["field"] != "name" {
		t.Errorf("error details field = %v, want %q", de.Details["field"], "name")
	}
}

func TestCreate_RejectsANameOver255Characters(t *testing.T) {
	f := newFacade()
	tooLong := make([]byte, organizations.NameMaxLength+1)
	for i := range tooLong {
		tooLong[i] = 'a'
	}

	_, err := f.Create(context.Background(), string(tooLong))

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)
}

func TestGet_ReturnsAPreviouslyCreatedOrganization(t *testing.T) {
	f := newFacade()
	created, err := f.Create(context.Background(), "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Get(context.Background(), created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Errorf("Get() = %+v, want %+v", got, created)
	}
}

func TestGet_ReturnsTypedNotFoundForAnUnknownID(t *testing.T) {
	f := newFacade()

	_, err := f.Get(context.Background(), organizations.OrgID("org_missing"))

	assertDomainError(t, err, organizations.CodeNotFound, 404)
}
