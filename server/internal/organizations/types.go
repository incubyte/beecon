// Package organizations owns the Organization entity and the installation's
// data-isolation unit. OrgID is a distinct type — never a raw string — so
// every org-scoped port method in every other module fails to compile unless
// it takes an OrgID, closing off the "forgot the WHERE clause" class of bug.
package organizations

import (
	"fmt"
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

// DefaultFeaturedCap is the onboarding "featured" integration list's default
// cap (PD43) — rolai's own limit of 8, applied whenever an org's governance
// leaves FeaturedCap unset (<= 0).
const DefaultFeaturedCap = 8

// Governance is one organization's integration allow-list, per-integration
// visibility, and onboarding "featured" configuration (PD42/PD43) — a
// sibling aggregate to Organization, keyed by the same OrgID and persisted in
// its own org_governance row. AllowList nil means "inherit the full
// installation catalog" (PD42, continuity-preserving): an organization that
// has never been configured — every organization before this phase shipped —
// sees exactly what it saw before, and upgrading changes nothing until an
// operator curates it. AllowList non-nil (even an empty slice) restricts the
// org to exactly the listed integration ids, still minus anything Hidden.
// Featured is an ordered subset of visible integration ids surfaced first
// during onboarding (PD43), capped at FeaturedCap. LogRetentionDays and
// EventRetentionDays (Slice 7, PD44) are each nil, 0, or a positive integer:
// nil means "inherit the installation's own BEECON_RETENTION_DAYS default";
// 0 means unlimited/disabled — the purge worker never purges this org's
// rows for that entity kind, regardless of age; a positive value overrides
// the installation default with this org's own window, in days. They are
// set only through WithRetention, never through NewGovernance/
// NewDefaultGovernance directly, so a governance-only PUT (SetGovernance)
// and a retention-only PUT (SetRetention) can each replace their own half of
// this shared settings record (FD8) without disturbing the other's fields.
type Governance struct {
	OrgID              OrgID
	AllowList          *[]string
	Hidden             []string
	Featured           []string
	FeaturedCap        int
	LogRetentionDays   *int
	EventRetentionDays *int
}

// NewDefaultGovernance returns org's continuity-preserving default
// governance (PD42): no allow-list (inherit the full catalog), nothing
// hidden, no featured list, and the platform's default featured cap — what
// GetGovernance synthesizes for an organization with no governance row.
func NewDefaultGovernance(org OrgID) Governance {
	return Governance{OrgID: org, FeaturedCap: DefaultFeaturedCap}
}

// NewGovernance validates and constructs a Governance for org, applying
// DefaultFeaturedCap when featuredCap is unset (<= 0) and rejecting a
// featured list longer than the effective cap (PD43). allowList nil is
// preserved as nil (PD42's "inherit all" state); a non-nil allowList is
// copied so the caller's own slice can't be mutated out from under the
// stored value.
func NewGovernance(org OrgID, allowList *[]string, hidden, featured []string, featuredCap int) (Governance, error) {
	cap := featuredCap
	if cap <= 0 {
		cap = DefaultFeaturedCap
	}
	if len(featured) > cap {
		return Governance{}, ErrValidation("featured", fmt.Sprintf("exceeds the featured cap of %d", cap))
	}
	return Governance{
		OrgID:       org,
		AllowList:   copyAllowList(allowList),
		Hidden:      copyStrings(hidden),
		Featured:    copyStrings(featured),
		FeaturedCap: cap,
	}, nil
}

func copyAllowList(allowList *[]string) *[]string {
	if allowList == nil {
		return nil
	}
	copied := copyStrings(*allowList)
	return &copied
}

func copyStrings(values []string) []string {
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

func copyIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// MinRetentionDays is the platform-wide floor a per-org retention window
// must clear (Slice 7, AC5) — unless it is exactly 0, PD44's dedicated
// "unlimited/disabled" escape hatch, which is always accepted regardless of
// this floor. No compliance mandate is named for Beecon itself (PD44 names
// no specific number), so the floor is deliberately small: its purpose is
// to reject nonsensical windows (negative values, off-by-a-sign typos), not
// to impose a business retention policy Beecon has no basis for.
const MinRetentionDays = 1

// validateRetentionWindow rejects a non-nil, non-zero days pointer below
// MinRetentionDays; nil (inherit the installation default) and 0
// (unlimited/disabled) are always accepted.
func validateRetentionWindow(field string, days *int) error {
	if days == nil || *days == 0 {
		return nil
	}
	if *days < MinRetentionDays {
		return ErrValidation(field, fmt.Sprintf("must be 0 (unlimited) or at least %d day(s)", MinRetentionDays))
	}
	return nil
}

// WithRetention returns a copy of g with its own log/event retention
// windows replaced by logDays/eventDays (Slice 7, PD44), leaving every
// other field (AllowList/Hidden/Featured/FeaturedCap) untouched — the
// retention-only half of org_governance's whole-replace convention, mirrored
// by Facade.SetRetention. nil means inherit the installation's own
// BEECON_RETENTION_DAYS default; 0 means unlimited/disabled for this
// organization; any other value below MinRetentionDays is rejected.
func (g Governance) WithRetention(logDays, eventDays *int) (Governance, error) {
	if err := validateRetentionWindow("logRetentionDays", logDays); err != nil {
		return Governance{}, err
	}
	if err := validateRetentionWindow("eventRetentionDays", eventDays); err != nil {
		return Governance{}, err
	}
	updated := g
	updated.LogRetentionDays = copyIntPtr(logDays)
	updated.EventRetentionDays = copyIntPtr(eventDays)
	return updated, nil
}

// EffectiveLogRetentionDays resolves g's own log-retention override against
// installationDefault (PD44, Slice 7): nil means inherit the installation's
// own BEECON_RETENTION_DAYS default; 0 (explicitly set) means unlimited —
// the purge worker never deletes this org's log entries, regardless of age.
func (g Governance) EffectiveLogRetentionDays(installationDefault int) int {
	if g.LogRetentionDays == nil {
		return installationDefault
	}
	return *g.LogRetentionDays
}

// EffectiveEventRetentionDays is EffectiveLogRetentionDays' mirror for
// terminal outbox events (PD44, Slice 7).
func (g Governance) EffectiveEventRetentionDays(installationDefault int) int {
	if g.EventRetentionDays == nil {
		return installationDefault
	}
	return *g.EventRetentionDays
}

// IsHidden reports whether integrationID is on g's explicit hidden set —
// hidden always wins regardless of AllowList (an operator hiding a
// specifically allow-listed integration still hides it).
func (g Governance) IsHidden(integrationID string) bool {
	return containsString(g.Hidden, integrationID)
}

// IsVisible reports whether an organization governed by g may see
// integrationID (PD42): hidden entries are never visible; with no allow-list
// (nil) every non-hidden integration is visible (continuity's default); with
// an allow-list set, only listed (and non-hidden) integrations are visible.
func (g Governance) IsVisible(integrationID string) bool {
	if g.IsHidden(integrationID) {
		return false
	}
	if g.AllowList == nil {
		return true
	}
	return containsString(*g.AllowList, integrationID)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
