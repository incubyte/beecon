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
