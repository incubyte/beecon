// Package organizations owns the Organization entity and the installation's
// data-isolation unit. OrgID is a distinct type — never a raw string — so
// every org-scoped port method in every other module fails to compile unless
// it takes an OrgID, closing off the "forgot the WHERE clause" class of bug.
package organizations

import (
	"net/url"
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
	ID                  OrgID
	Name                string
	AllowedRedirectURIs []string
	CreatedAt           time.Time
}

// NewOrganization validates name and constructs an Organization. The id is
// supplied by the caller's newID func so the id-format ownership stays in the
// wiring, not the domain type. The allow-list starts empty (PD4: an empty
// list rejects every redirectUri — a secure default, no open redirect) until
// the installation admin sets it via SetAllowedRedirectURIs.
func NewOrganization(id OrgID, name string, now time.Time) (Organization, error) {
	trimmed := strings.TrimSpace(name)
	if err := validateName(trimmed); err != nil {
		return Organization{}, err
	}
	return Organization{
		ID:                  id,
		Name:                trimmed,
		AllowedRedirectURIs: []string{},
		CreatedAt:           now,
	}, nil
}

// WithAllowedRedirectURIs returns a copy of o with its redirect-uri allow-list
// replaced by uris (PD4), trimmed of surrounding whitespace and blank
// entries.
func (o Organization) WithAllowedRedirectURIs(uris []string) Organization {
	updated := o
	updated.AllowedRedirectURIs = normalizeRedirectURIs(uris)
	return updated
}

func normalizeRedirectURIs(uris []string) []string {
	normalized := make([]string, 0, len(uris))
	for _, uri := range uris {
		trimmed := strings.TrimSpace(uri)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

// AllowsRedirectURI reports whether candidate is permitted by o's allow-list
// (PD4). An empty allow-list always rejects — the secure default that rules
// out an open redirect. Each allowed entry matches either as an exact URL, or
// (when the entry carries no path of its own) as an origin: candidate is
// allowed if its scheme and host match the entry's.
func (o Organization) AllowsRedirectURI(candidate string) bool {
	for _, allowed := range o.AllowedRedirectURIs {
		if redirectURIMatches(allowed, candidate) {
			return true
		}
	}
	return false
}

func redirectURIMatches(allowed, candidate string) bool {
	if allowed == candidate {
		return true
	}
	allowedURL, err := url.Parse(allowed)
	if err != nil || !isOriginOnly(allowedURL) {
		return false
	}
	candidateURL, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	return allowedURL.Scheme == candidateURL.Scheme && allowedURL.Host == candidateURL.Host
}

func isOriginOnly(u *url.URL) bool {
	return u.Path == "" || u.Path == "/"
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
