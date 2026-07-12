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

// toolProviderDTO is the provider identity nested inside a toolSummaryDTO
// (API Shape): a consumer addressing tools by slug alone (PD8) still needs
// to know which provider a tool belongs to.
type toolProviderDTO struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Logo string `json:"logo"`
}

// toolSummaryDTO is one tool as GET /api/v1/tools and GET
// /api/v1/tools/{slug} return it.
type toolSummaryDTO struct {
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  map[string]any  `json:"inputSchema"`
	OutputSchema map[string]any  `json:"outputSchema"`
	Deprecated   bool            `json:"deprecated"`
	Provider     toolProviderDTO `json:"provider"`
}

// toolsPageDTO is one cursor-paginated page of tools (PD15): nextCursor is
// empty when this was the last page.
type toolsPageDTO struct {
	Items      []toolSummaryDTO `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

func toToolSummaryDTO(tool catalog.ToolSummary) toolSummaryDTO {
	return toolSummaryDTO{
		Slug:         tool.Slug,
		Name:         tool.Name,
		Description:  tool.Description,
		InputSchema:  tool.InputSchema,
		OutputSchema: tool.OutputSchema,
		Deprecated:   tool.Deprecated,
		Provider: toolProviderDTO{
			Slug: tool.ProviderSlug,
			Name: tool.ProviderName,
			Logo: tool.ProviderLogo,
		},
	}
}

func toToolsPageDTO(page catalog.ToolPage) toolsPageDTO {
	items := make([]toolSummaryDTO, 0, len(page.Items))
	for _, tool := range page.Items {
		items = append(items, toToolSummaryDTO(tool))
	}
	return toolsPageDTO{Items: items, NextCursor: page.NextCursor}
}
