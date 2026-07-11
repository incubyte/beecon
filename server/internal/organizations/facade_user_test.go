// Package organizations_test (see facade_test.go's header for why this must
// be an external test package). This file covers the User entity added in
// Slice 2 (PD2): reuses newFacade/assertDomainError already declared in
// facade_test.go.
package organizations_test

import (
	"context"
	"testing"

	"beecon/internal/organizations"
)

func TestCreateUser_MintsAUserPrefixedIDDeterministically(t *testing.T) {
	f := newFacade()

	user, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "Ada Lovelace", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(user.ID) != "user_1" {
		t.Errorf("ID = %q, want %q (deterministic sequential id from the memory fake)", user.ID, "user_1")
	}
}

func TestCreateUser_IDsAreSequentialAcrossMultipleCreates(t *testing.T) {
	f := newFacade()

	first, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "First", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "Second", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(first.ID) != "user_1" {
		t.Errorf("first.ID = %q, want %q", first.ID, "user_1")
	}
	if string(second.ID) != "user_2" {
		t.Errorf("second.ID = %q, want %q", second.ID, "user_2")
	}
}

func TestCreateUser_StoresTheOrgNameAndOptionalExternalID(t *testing.T) {
	f := newFacade()
	org := organizations.OrgID("org_1")

	user, err := f.CreateUser(context.Background(), org, "Ada Lovelace", "ext-42")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.OrgID != org {
		t.Errorf("OrgID = %q, want %q", user.OrgID, org)
	}
	if user.Name != "Ada Lovelace" {
		t.Errorf("Name = %q, want %q", user.Name, "Ada Lovelace")
	}
	if user.ExternalID != "ext-42" {
		t.Errorf("ExternalID = %q, want %q", user.ExternalID, "ext-42")
	}
}

func TestCreateUser_ExternalIDIsOptionalAndDefaultsToEmpty(t *testing.T) {
	f := newFacade()

	user, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "Ada Lovelace", "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user.ExternalID != "" {
		t.Errorf("ExternalID = %q, want empty string when omitted", user.ExternalID)
	}
}

func TestCreateUser_RejectsAnEmptyName(t *testing.T) {
	f := newFacade()

	_, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "", "")

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)
}

func TestCreateUser_RejectsAWhitespaceOnlyName(t *testing.T) {
	f := newFacade()

	_, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "   ", "")

	assertDomainError(t, err, organizations.CodeValidationFailed, 422)
}

func TestGetUser_ReturnsAUserCreatedEarlierInItsOwnOrg(t *testing.T) {
	f := newFacade()
	org := organizations.OrgID("org_1")
	created, err := f.CreateUser(context.Background(), org, "Ada Lovelace", "ext-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := f.GetUser(context.Background(), org, created.ID)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != created {
		t.Errorf("GetUser() = %+v, want %+v", got, created)
	}
}

func TestGetUser_ReturnsTypedNotFoundForAnUnknownID(t *testing.T) {
	f := newFacade()

	_, err := f.GetUser(context.Background(), organizations.OrgID("org_1"), organizations.UserID("user_missing"))

	assertDomainError(t, err, organizations.CodeNotFound, 404)
}

func TestGetUser_ReturnsTypedNotFoundForAUserBelongingToAnotherOrg(t *testing.T) {
	f := newFacade()
	created, err := f.CreateUser(context.Background(), organizations.OrgID("org_1"), "Ada Lovelace", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = f.GetUser(context.Background(), organizations.OrgID("org_2"), created.ID)

	assertDomainError(t, err, organizations.CodeNotFound, 404)
}
