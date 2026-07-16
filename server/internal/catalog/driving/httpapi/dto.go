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

// integrationSummaryListDTO is ListIntegrationsForProvider's response
// envelope: low cardinality (one provider's installation-level integrations)
// so no cursor pagination is needed, unlike the tools/trigger-definitions
// pages.
type integrationSummaryListDTO struct {
	Items []integrationSummaryDTO `json:"items"`
}

func toIntegrationSummaryListDTO(summaries []catalog.IntegrationSummary) integrationSummaryListDTO {
	return integrationSummaryListDTO{Items: toIntegrationSummaryDTOs(summaries)}
}

// integrationVisibilityDTO is ListWithVisibility's response shape (Slice 5,
// AC1): the same integration identity fields as integrationSummaryDTO plus
// its effective visibility for the requested org.
type integrationVisibilityDTO struct {
	ID           string `json:"id"`
	ProviderSlug string `json:"providerSlug"`
	Name         string `json:"name"`
	Logo         string `json:"logo"`
	AuthScheme   string `json:"authScheme"`
	Visibility   string `json:"visibility"`
}

func toIntegrationVisibilityDTO(item catalog.IntegrationVisibility) integrationVisibilityDTO {
	summary := toIntegrationSummaryDTO(item.Integration)
	return integrationVisibilityDTO{
		ID:           summary.ID,
		ProviderSlug: summary.ProviderSlug,
		Name:         summary.Name,
		Logo:         summary.Logo,
		AuthScheme:   summary.AuthScheme,
		Visibility:   item.Visibility,
	}
}

func toIntegrationVisibilityDTOs(items []catalog.IntegrationVisibility) []integrationVisibilityDTO {
	dtos := make([]integrationVisibilityDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toIntegrationVisibilityDTO(item))
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

// expectedParamFieldDTO is one field of GET
// /api/v1/integrations/{intgId}/expected-params' response (Slice 3, AC2):
// never a value — only the field's own shape (name, display label,
// description, required/secret flags).
type expectedParamFieldDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

// expectedParamsDTO is GET /api/v1/integrations/{intgId}/expected-params'
// response shape (Slice 3, AC2): the provider's name plus its expected
// fields.
type expectedParamsDTO struct {
	ProviderName string                  `json:"providerName"`
	Fields       []expectedParamFieldDTO `json:"fields"`
}

func toExpectedParamsDTO(view catalog.ExpectedParamsView) expectedParamsDTO {
	fields := make([]expectedParamFieldDTO, 0, len(view.Fields))
	for _, field := range view.Fields {
		fields = append(fields, expectedParamFieldDTO{
			Name:        field.Name,
			DisplayName: field.DisplayName,
			Description: field.Description,
			Required:    field.Required,
			Secret:      field.Secret,
		})
	}
	return expectedParamsDTO{ProviderName: view.ProviderName, Fields: fields}
}

func toToolsPageDTO(page catalog.ToolPage) toolsPageDTO {
	items := make([]toolSummaryDTO, 0, len(page.Items))
	for _, tool := range page.Items {
		items = append(items, toToolSummaryDTO(tool))
	}
	return toolsPageDTO{Items: items, NextCursor: page.NextCursor}
}

// triggerDefinitionProviderDTO is the provider identity nested inside a
// triggerDefinitionSummaryDTO (API Shape) — mirrors toolProviderDTO: a
// consumer addressing trigger definitions by slug alone (PD14) still needs
// to know which provider a trigger belongs to.
type triggerDefinitionProviderDTO struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Logo string `json:"logo"`
}

// triggerDefinitionSummaryDTO is one trigger definition as GET
// /api/v1/trigger-definitions and GET /api/v1/trigger-definitions/{slug}
// return it (API Shape).
type triggerDefinitionSummaryDTO struct {
	Slug          string                       `json:"slug"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	ConfigSchema  map[string]any               `json:"configSchema"`
	PayloadSchema map[string]any               `json:"payloadSchema"`
	Ingestion     string                       `json:"ingestion"`
	Provider      triggerDefinitionProviderDTO `json:"provider"`
}

// triggerDefinitionsPageDTO is one cursor-paginated page of trigger
// definitions (PD15): nextCursor is empty when this was the last page.
type triggerDefinitionsPageDTO struct {
	Items      []triggerDefinitionSummaryDTO `json:"items"`
	NextCursor string                        `json:"nextCursor,omitempty"`
}

func toTriggerDefinitionSummaryDTO(trigger catalog.TriggerDefinitionSummary) triggerDefinitionSummaryDTO {
	return triggerDefinitionSummaryDTO{
		Slug:          trigger.Slug,
		Name:          trigger.Name,
		Description:   trigger.Description,
		ConfigSchema:  trigger.ConfigSchema,
		PayloadSchema: trigger.PayloadSchema,
		Ingestion:     trigger.Ingestion,
		Provider: triggerDefinitionProviderDTO{
			Slug: trigger.ProviderSlug,
			Name: trigger.ProviderName,
			Logo: trigger.ProviderLogo,
		},
	}
}

func toTriggerDefinitionsPageDTO(page catalog.TriggerDefinitionPage) triggerDefinitionsPageDTO {
	items := make([]triggerDefinitionSummaryDTO, 0, len(page.Items))
	for _, trigger := range page.Items {
		items = append(items, toTriggerDefinitionSummaryDTO(trigger))
	}
	return triggerDefinitionsPageDTO{Items: items, NextCursor: page.NextCursor}
}

// providerDefinitionSummaryDTO is one row of GET /api/v1/provider-definitions
// (PD40, Slice 6, AC1): the operator's unfiltered, installation-wide view —
// name/logo/authScheme identify the provider, formatVersion names the
// definition format it was parsed under, and toolCount/triggerCount let an
// operator scan the catalog without opening every provider's full detail.
type providerDefinitionSummaryDTO struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Logo          string `json:"logo"`
	AuthScheme    string `json:"authScheme"`
	FormatVersion int    `json:"formatVersion"`
	ToolCount     int    `json:"toolCount"`
	TriggerCount  int    `json:"triggerCount"`
}

func toProviderDefinitionSummaryDTO(summary catalog.ProviderDefinitionSummary) providerDefinitionSummaryDTO {
	return providerDefinitionSummaryDTO{
		Slug:          summary.Slug,
		Name:          summary.Name,
		Logo:          summary.Logo,
		AuthScheme:    summary.AuthScheme,
		FormatVersion: summary.FormatVersion,
		ToolCount:     summary.ToolCount,
		TriggerCount:  summary.TriggerCount,
	}
}

// providerDefinitionsPageDTO is one cursor-paginated page of provider
// definition summaries (PD15): nextCursor is empty when this was the last
// page.
type providerDefinitionsPageDTO struct {
	Items      []providerDefinitionSummaryDTO `json:"items"`
	NextCursor string                         `json:"nextCursor,omitempty"`
}

func toProviderDefinitionsPageDTO(page catalog.ProviderDefinitionPage) providerDefinitionsPageDTO {
	items := make([]providerDefinitionSummaryDTO, 0, len(page.Items))
	for _, summary := range page.Items {
		items = append(items, toProviderDefinitionSummaryDTO(summary))
	}
	return providerDefinitionsPageDTO{Items: items, NextCursor: page.NextCursor}
}

// providerDefinitionDetailDTO is GET /api/v1/provider-definitions/{slug}'s
// response (PD40, Slice 6, AC2): the definition's full versioned bundle,
// rendered client-side in a collapsible, copyable mono JSON/YAML viewer.
type providerDefinitionDetailDTO struct {
	Slug          string         `json:"slug"`
	Name          string         `json:"name"`
	FormatVersion int            `json:"formatVersion"`
	Bundle        map[string]any `json:"bundle"`
}

func toProviderDefinitionDetailDTO(detail catalog.ProviderDefinitionBundleDetail) providerDefinitionDetailDTO {
	return providerDefinitionDetailDTO{
		Slug:          detail.Slug,
		Name:          detail.Name,
		FormatVersion: detail.FormatVersion,
		Bundle:        detail.Bundle,
	}
}
