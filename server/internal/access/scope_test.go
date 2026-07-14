// Package access_test (see facade_test.go's own header for why this is an
// external test package, and its shared newFacade/orgA/orgB/assertDomainError
// helpers reused here). This file covers PD41/Slice 4's Scope: ParseScope's
// normalization/validation rules, Scope.IsReadOnly, and that Issue/Verify/List
// carry a caller-chosen scope through end to end rather than silently
// collapsing every key to read-write.
package access_test

import (
	"context"
	"testing"

	"beecon/internal/access"
)

func TestParseScope_DefaultsAnEmptyStringToReadWrite(t *testing.T) {
	got, err := access.ParseScope("")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != access.ScopeReadWrite {
		t.Errorf("ParseScope(\"\") = %q, want %q (every pre-existing caller that omits scope keeps full access)", got, access.ScopeReadWrite)
	}
}

func TestParseScope_AcceptsReadOnly(t *testing.T) {
	got, err := access.ParseScope("read-only")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != access.ScopeReadOnly {
		t.Errorf("ParseScope(\"read-only\") = %q, want %q", got, access.ScopeReadOnly)
	}
}

func TestParseScope_AcceptsReadWrite(t *testing.T) {
	got, err := access.ParseScope("read-write")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != access.ScopeReadWrite {
		t.Errorf("ParseScope(\"read-write\") = %q, want %q", got, access.ScopeReadWrite)
	}
}

func TestParseScope_RejectsAnyOtherValueAsAValidationError(t *testing.T) {
	_, err := access.ParseScope("admin")

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

// TestParseScope_RejectsACaseVariantOfAKnownScope pins that scope matching is
// exact — "Read-Only"/"READ-ONLY" must not silently coerce to the real
// enum value, since that would make RequireWrite's check bypassable by a
// caller who capitalizes the field differently than the two documented
// values.
func TestParseScope_RejectsACaseVariantOfAKnownScope(t *testing.T) {
	_, err := access.ParseScope("Read-Only")

	assertDomainError(t, err, access.CodeValidationFailed, 422)
}

func TestScopeIsReadOnly_TrueOnlyForTheReadOnlyScope(t *testing.T) {
	if !access.ScopeReadOnly.IsReadOnly() {
		t.Error("ScopeReadOnly.IsReadOnly() = false, want true")
	}
	if access.ScopeReadWrite.IsReadOnly() {
		t.Error("ScopeReadWrite.IsReadOnly() = true, want false")
	}
}

func TestIssue_WithReadOnlyScopeReturnsItInTheIssuedKey(t *testing.T) {
	f := newFacade()

	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadOnly)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued.Scope != access.ScopeReadOnly {
		t.Errorf("issued.Scope = %q, want %q", issued.Scope, access.ScopeReadOnly)
	}
}

func TestIssue_WithReadWriteScopeReturnsItInTheIssuedKey(t *testing.T) {
	f := newFacade()

	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued.Scope != access.ScopeReadWrite {
		t.Errorf("issued.Scope = %q, want %q", issued.Scope, access.ScopeReadWrite)
	}
}

// TestVerify_RoundTripsTheReadOnlyScopeIssuedForTheKey is FD4's core
// guarantee: a key issued read-only must still be reported read-only by
// Verify — this is exactly what authmw.RequireWrite depends on to reject a
// mutating call.
func TestVerify_RoundTripsTheReadOnlyScopeIssuedForTheKey(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadOnly)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Verify(context.Background(), issued.Secret)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Scope != access.ScopeReadOnly {
		t.Errorf("Verify() scope = %q, want %q", got.Scope, access.ScopeReadOnly)
	}
}

// TestVerify_RoundTripsTheReadWriteScopeIssuedForTheKey mirrors the read-only
// case above for the opposite value, so the round trip is proven for both
// enum members rather than just the one RequireWrite actually rejects.
func TestVerify_RoundTripsTheReadWriteScopeIssuedForTheKey(t *testing.T) {
	f := newFacade()
	issued, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.Verify(context.Background(), issued.Secret)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Scope != access.ScopeReadWrite {
		t.Errorf("Verify() scope = %q, want %q", got.Scope, access.ScopeReadWrite)
	}
}

// TestList_SurfacesEachKeysOwnScopeNeverAnothersKeysScope confirms List's
// KeyListing carries the right scope per key when an org holds one of each,
// not just a single-key happy path.
func TestList_SurfacesEachKeysOwnScopeNeverAnothersKeysScope(t *testing.T) {
	f := newFacade()
	readOnly, err := f.Issue(context.Background(), orgA, access.ScopeReadOnly)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	readWrite, err := f.Issue(context.Background(), orgA, access.ScopeReadWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keys, err := f.List(context.Background(), orgA)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	scopesByID := map[access.KeyID]access.Scope{}
	for _, k := range keys {
		scopesByID[k.ID] = k.Scope
	}
	if scopesByID[readOnly.ID] != access.ScopeReadOnly {
		t.Errorf("List()'s scope for the read-only key = %q, want %q", scopesByID[readOnly.ID], access.ScopeReadOnly)
	}
	if scopesByID[readWrite.ID] != access.ScopeReadWrite {
		t.Errorf("List()'s scope for the read-write key = %q, want %q", scopesByID[readWrite.ID], access.ScopeReadWrite)
	}
}
