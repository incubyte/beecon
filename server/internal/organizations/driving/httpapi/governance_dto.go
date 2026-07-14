package httpapi

import "beecon/internal/organizations"

// onboardingDTO is governanceDTO's nested onboarding shape (Slice 5, PD43):
// the ordered featured integration ids plus the effective cap they're
// bounded by.
type onboardingDTO struct {
	Featured []string `json:"featured"`
	Cap      int      `json:"cap"`
}

// governanceDTO is GET/PUT .../governance's response shape (Slice 5,
// PD42/PD43): AllowList is nil (JSON null) when the org inherits the full
// installation catalog — the same tri-state a request body carries back in.
type governanceDTO struct {
	AllowList  *[]string     `json:"allowList"`
	Hidden     []string      `json:"hidden"`
	Onboarding onboardingDTO `json:"onboarding"`
}

func toGovernanceDTO(governance organizations.Governance) governanceDTO {
	return governanceDTO{
		AllowList: governance.AllowList,
		Hidden:    nonNilStrings(governance.Hidden),
		Onboarding: onboardingDTO{
			Featured: nonNilStrings(governance.Featured),
			Cap:      governance.FeaturedCap,
		},
	}
}

// nonNilStrings never serializes a governance list field as JSON null — an
// unconfigured org's empty Hidden/Featured slices render as [], matching
// what a PUT round-trip would send back.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// onboardingRequest is governanceRequest's nested onboarding shape.
type onboardingRequest struct {
	Featured []string `json:"featured"`
	Cap      int      `json:"cap"`
}

// governanceRequest is PUT .../governance's request body (Slice 5): it
// replaces the org's entire governance record (mirrors
// updateAllowedRedirectURIsRequest's own whole-replace convention).
// AllowList absent or JSON null decodes to a nil pointer ("inherit the full
// catalog", PD42); AllowList present (even `[]`) decodes to a non-nil
// pointer restricting the org to exactly the listed ids.
type governanceRequest struct {
	AllowList  *[]string         `json:"allowList"`
	Hidden     []string          `json:"hidden"`
	Onboarding onboardingRequest `json:"onboarding"`
}

func (req governanceRequest) toUpdate() organizations.GovernanceUpdate {
	return organizations.GovernanceUpdate{
		AllowList:   req.AllowList,
		Hidden:      req.Hidden,
		Featured:    req.Onboarding.Featured,
		FeaturedCap: req.Onboarding.Cap,
	}
}
