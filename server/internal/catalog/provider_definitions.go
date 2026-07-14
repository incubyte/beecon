package catalog

import (
	"context"
	"sort"
)

// ListProviderDefinitions returns the installation's loaded provider
// definitions (PD40, Slice 6, AC1), sorted by slug and cursor-paginated over
// that sort order — mirrors ListTools/ListTriggerDefinitions' in-memory
// pagination (ADR-0006), since provider definitions are boot-loaded, not
// database rows. Deliberately takes no organization: this is the operator's
// installation-wide, un-governance-filtered view of the real installed
// estate (AC7) — every organization's allow-list/hidden set is irrelevant
// here.
func (f *Facade) ListProviderDefinitions(_ context.Context, cursor string, limit int) (ProviderDefinitionPage, error) {
	after, err := decodeSlugCursor(cursor)
	if err != nil {
		return ProviderDefinitionPage{}, err
	}

	summaries := make([]ProviderDefinitionSummary, 0, len(f.definitions))
	for _, definition := range f.definitions {
		summaries = append(summaries, providerDefinitionSummaryFrom(definition))
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Slug < summaries[j].Slug })
	summaries = providerDefinitionsAfterCursor(summaries, after)

	return paginateProviderDefinitions(summaries, normalizePageLimit(limit)), nil
}

// ProviderDefinitionDetail returns providerSlug's full versioned Bundle
// (PD40, Slice 6, AC2) — an unknown slug is ErrProviderNotFound, the same
// not-found ListTools/ListTriggerDefinitions already use for an unknown
// provider filter.
func (f *Facade) ProviderDefinitionDetail(_ context.Context, providerSlug string) (ProviderDefinitionBundleDetail, error) {
	definition, ok := f.definitions[providerSlug]
	if !ok {
		return ProviderDefinitionBundleDetail{}, ErrProviderNotFound()
	}
	return ProviderDefinitionBundleDetail{
		Slug:          definition.Slug,
		Name:          definition.Name,
		FormatVersion: supportedFormatVersion,
		Bundle:        providerDefinitionBundle(definition),
	}, nil
}

func providerDefinitionSummaryFrom(definition ProviderDefinition) ProviderDefinitionSummary {
	return ProviderDefinitionSummary{
		Slug:          definition.Slug,
		Name:          definition.Name,
		Logo:          definition.Logo,
		AuthScheme:    definition.AuthScheme,
		FormatVersion: supportedFormatVersion,
		ToolCount:     len(definition.Tools),
		TriggerCount:  len(definition.Triggers),
	}
}

// providerDefinitionsAfterCursor returns the definitions sorted strictly
// after the cursor's slug — mirrors toolsAfterCursor/triggerDefinitionsAfterCursor.
func providerDefinitionsAfterCursor(definitions []ProviderDefinitionSummary, after string) []ProviderDefinitionSummary {
	if after == "" {
		return definitions
	}
	idx := sort.Search(len(definitions), func(i int) bool { return definitions[i].Slug > after })
	return definitions[idx:]
}

func paginateProviderDefinitions(definitions []ProviderDefinitionSummary, limit int) ProviderDefinitionPage {
	hasMore := len(definitions) > limit
	if hasMore {
		definitions = definitions[:limit]
	}
	page := ProviderDefinitionPage{Items: definitions}
	if hasMore {
		page.NextCursor = encodeSlugCursor(definitions[len(definitions)-1].Slug)
	}
	return page
}

// providerDefinitionBundle renders definition as the JSON-serializable
// bundle an operator inspects (PD40, Slice 6, AC2): it reconstructs the
// finalized definition format's on-disk shape (PD13) from the parsed
// domain ProviderDefinition, so the bundle an operator reads matches the
// provider definition file that produced it, field for field, including
// every tool's and trigger's mapping.
func providerDefinitionBundle(definition ProviderDefinition) map[string]any {
	return map[string]any{
		"formatVersion": supportedFormatVersion,
		"slug":          definition.Slug,
		"name":          definition.Name,
		"logo":          definition.Logo,
		"authScheme":    definition.AuthScheme,
		"oauth": map[string]any{
			"authorizeUrl":    definition.AuthorizeURL,
			"tokenUrl":        definition.TokenURL,
			"userInfoUrl":     definition.UserInfoURL,
			"scopes":          definition.Scopes,
			"credentialStyle": definition.CredentialStyle,
			"userInfo": map[string]any{
				"email":       definition.UserInfo.EmailField,
				"displayName": definition.UserInfo.DisplayNameField,
			},
		},
		"mapping": map[string]any{
			"baseUrl": definition.BaseURL,
		},
		"expectedParams": expectedParamsBundle(definition.ExpectedParams),
		"tools":          toolsBundle(definition.Tools),
		"triggers":       triggersBundle(definition.Triggers),
	}
}

func expectedParamsBundle(params []ExpectedParam) []map[string]any {
	bundle := make([]map[string]any, 0, len(params))
	for _, param := range params {
		bundle = append(bundle, map[string]any{
			"name":        param.Name,
			"displayName": param.DisplayName,
			"description": param.Description,
			"required":    param.Required,
			"secret":      param.Secret,
		})
	}
	return bundle
}

func toolsBundle(tools []ProviderTool) []map[string]any {
	bundle := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		bundle = append(bundle, map[string]any{
			"slug":         tool.Slug,
			"name":         tool.Name,
			"description":  tool.Description,
			"deprecated":   tool.Deprecated,
			"inputSchema":  tool.InputSchema,
			"outputSchema": tool.OutputSchema,
			"mapping": map[string]any{
				"method": tool.Method,
				"path":   tool.Path,
				"query":  tool.Mapping.Query,
				"header": tool.Mapping.Header,
				"body":   tool.Mapping.Body,
			},
		})
	}
	return bundle
}

func triggersBundle(triggers []TriggerDefinition) []map[string]any {
	bundle := make([]map[string]any, 0, len(triggers))
	for _, trigger := range triggers {
		bundle = append(bundle, map[string]any{
			"slug":                trigger.Slug,
			"name":                trigger.Name,
			"description":         trigger.Description,
			"configSchema":        trigger.ConfigSchema,
			"payloadSchema":       trigger.PayloadSchema,
			"ingestion":           trigger.Ingestion,
			"pollIntervalSeconds": trigger.PollIntervalSeconds,
			"poll": map[string]any{
				"method":              trigger.Poll.Method,
				"path":                trigger.Poll.Path,
				"query":               trigger.Poll.Query,
				"body":                trigger.Poll.Body,
				"recordsPath":         trigger.Poll.RecordsPath,
				"recordIdPath":        trigger.Poll.RecordIDPath,
				"recordTimestampPath": trigger.Poll.RecordTimestampPath,
				"payload":             trigger.Poll.Payload,
			},
		})
	}
	return bundle
}
