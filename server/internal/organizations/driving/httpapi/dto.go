package httpapi

import "beecon/internal/organizations"

type createOrganizationRequest struct {
	Name string `json:"name"`
}

// updateAllowedRedirectURIsRequest is the PATCH /api/v1/organizations/{orgId}
// body (PD4): it replaces the organization's entire redirect-uri allow-list.
type updateAllowedRedirectURIsRequest struct {
	AllowedRedirectUris []string `json:"allowedRedirectUris"`
}

type organizationDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	AllowedRedirectUris []string `json:"allowedRedirectUris"`
	CreatedAt           string   `json:"createdAt"`
}

func toOrganizationDTO(org organizations.Organization) organizationDTO {
	return organizationDTO{
		ID:                  string(org.ID),
		Name:                org.Name,
		AllowedRedirectUris: org.AllowedRedirectURIs,
		CreatedAt:           org.CreatedAt.Format(rfc3339Millis),
	}
}

const rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"
