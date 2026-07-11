package httpapi

import "beecon/internal/catalog"

// createIntegrationRequest is the POST /api/v1/integrations body (admin):
// clientSecret is written once and never echoed back (AC4).
type createIntegrationRequest struct {
	ProviderSlug string `json:"providerSlug"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// integrationSummaryDTO is the response to both Create and List: id,
// provider name, logo, auth scheme. It never carries the OAuth client secret
// (AC4).
type integrationSummaryDTO struct {
	ID           string `json:"id"`
	ProviderSlug string `json:"providerSlug"`
	Name         string `json:"name"`
	Logo         string `json:"logo"`
	AuthScheme   string `json:"authScheme"`
}

func toIntegrationSummaryDTO(summary catalog.IntegrationSummary) integrationSummaryDTO {
	return integrationSummaryDTO{
		ID:           string(summary.ID),
		ProviderSlug: summary.ProviderSlug,
		Name:         summary.ProviderName,
		Logo:         summary.Logo,
		AuthScheme:   summary.AuthScheme,
	}
}

func toIntegrationSummaryDTOs(summaries []catalog.IntegrationSummary) []integrationSummaryDTO {
	dtos := make([]integrationSummaryDTO, 0, len(summaries))
	for _, summary := range summaries {
		dtos = append(dtos, toIntegrationSummaryDTO(summary))
	}
	return dtos
}
