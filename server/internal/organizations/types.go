// Package organizations owns the Organization entity and the installation's
// data-isolation unit. OrgID is a distinct type — never a raw string — so
// every org-scoped port method in every other module fails to compile unless
// it takes an OrgID, closing off the "forgot the WHERE clause" class of bug.
package organizations

import (
	"strings"
	"time"
)

// OrgID is minted only by Create (and, from Slice 2, by access-key
// verification). No other code may construct one from an arbitrary string.
type OrgID string

// NameMaxLength is the longest an organization name may be.
const NameMaxLength = 255

// Organization is the domain aggregate root.
type Organization struct {
	ID        OrgID
	Name      string
	CreatedAt time.Time
}

// NewOrganization validates name and constructs an Organization. The id is
// supplied by the caller's newID func so the id-format ownership stays in the
// wiring, not the domain type.
func NewOrganization(id OrgID, name string, now time.Time) (Organization, error) {
	trimmed := strings.TrimSpace(name)
	if err := validateName(trimmed); err != nil {
		return Organization{}, err
	}
	return Organization{
		ID:        id,
		Name:      trimmed,
		CreatedAt: now,
	}, nil
}

func validateName(trimmed string) error {
	if trimmed == "" {
		return ErrInvalidName("name", "must not be empty")
	}
	if len(trimmed) > NameMaxLength {
		return ErrInvalidName("name", "must be at most 255 chars")
	}
	return nil
}

// UserID is minted only by CreateUser. A User always belongs to exactly one
// organization, carried on the User itself and required as the second
// parameter of every org-scoped UserRepository method.
type UserID string

// User is a consumer-provisioned entity scoped to one organization (PD2):
// the organization's own server creates its users with its org API key.
type User struct {
	ID         UserID
	OrgID      OrgID
	Name       string
	ExternalID string
	CreatedAt  time.Time
}

// NewUser validates name and constructs a User scoped to org. ExternalID is
// the consumer's own optional identifier and carries no validation.
func NewUser(id UserID, org OrgID, name, externalID string, now time.Time) (User, error) {
	trimmed := strings.TrimSpace(name)
	if err := validateName(trimmed); err != nil {
		return User{}, err
	}
	return User{
		ID:         id,
		OrgID:      org,
		Name:       trimmed,
		ExternalID: strings.TrimSpace(externalID),
		CreatedAt:  now,
	}, nil
}
