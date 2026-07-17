package membraneimport

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// convertedItem is one Membrane record this run successfully turned into
// part of a provider definition, for the report's Converted section.
type convertedItem struct {
	Source       string
	ProviderSlug string
	ItemSlug     string
	Kind         string // "tool" (Slice 1); "trigger" (Slice 5).
}

// providerKind is the partialItem.Kind used for a provider-level caveat (the
// Slice 3 OAuth/baseUrl TODO fallback) — distinct from "tool" and the
// Slice-5 "trigger" kind.
const providerKind = "provider"

// buildProviderDefinition renders one recordGroup's integration identity,
// convertible actions, and best-effort triggers into an emitted Beecon
// provider-definition file, plus the report items each tool/trigger
// contributes: a clean Converted item, a Partial item when the item
// converted but with dropped/defaulted constructs (every trigger that
// converts at all lands here — Slice 5's poll-mapping gap is structural, not
// incidental), or a SkippedItem when the item could not be converted at all
// (never a silent drop). The provider's own oauth/mapping.baseUrl block is
// filled from the Slice 3 known-provider preset table when the connector's
// identity matches one; an unmatched connector falls back to TODO
// placeholders and contributes its own "provider"-kind Partial item naming
// every TODO field.
func buildProviderDefinition(group recordGroup) (ProviderOutput, []convertedItem, []partialItem, []SkippedItem, error) {
	integrationFields := group.Integration.Fields
	slug := deriveSlug(stringAt(integrationFields, "key"), stringAt(integrationFields, "name"))
	if slug == "" {
		return ProviderOutput{}, nil, nil, nil, fmt.Errorf("could not derive a provider slug from integration key/name")
	}

	oauth, mapping, oauthCaveats := resolveOAuthAndMapping(integrationFields)
	definition := outputDefinitionV1{
		FormatVersion: 1,
		Slug:          slug,
		Name:          stringAt(integrationFields, "name"),
		Logo:          stringAt(integrationFields, "logoUri"),
		OAuth:         oauth,
		Mapping:       mapping,
	}

	var converted []convertedItem
	var partial []partialItem
	var skipped []SkippedItem
	if len(oauthCaveats) > 0 {
		partial = append(partial, partialItem{
			Source:       group.Integration.Name,
			ProviderSlug: slug,
			ItemSlug:     slug,
			Kind:         providerKind,
			Caveats:      oauthCaveats,
		})
	}
	for _, action := range group.Actions {
		tool, caveats, err := buildTool(action.Fields)
		if err != nil {
			skipped = append(skipped, SkippedItem{Source: action.Name, Reason: err.Error()})
			continue
		}
		definition.Tools = append(definition.Tools, tool)
		if len(caveats) > 0 {
			partial = append(partial, partialItem{
				Source:       action.Name,
				ProviderSlug: slug,
				ItemSlug:     tool.Slug,
				Kind:         "tool",
				Caveats:      caveats,
			})
			continue
		}
		converted = append(converted, convertedItem{
			Source:       action.Name,
			ProviderSlug: slug,
			ItemSlug:     tool.Slug,
			Kind:         "tool",
		})
	}
	for _, trigger := range group.Triggers {
		triggerDef, caveats, err := buildTrigger(trigger.Fields)
		if err != nil {
			skipped = append(skipped, SkippedItem{Source: trigger.Name, Reason: err.Error()})
			continue
		}
		definition.Triggers = append(definition.Triggers, triggerDef)
		if len(caveats) > 0 {
			partial = append(partial, partialItem{
				Source:       trigger.Name,
				ProviderSlug: slug,
				ItemSlug:     triggerDef.Slug,
				Kind:         "trigger",
				Caveats:      caveats,
			})
			continue
		}
		converted = append(converted, convertedItem{
			Source:       trigger.Name,
			ProviderSlug: slug,
			ItemSlug:     triggerDef.Slug,
			Kind:         "trigger",
		})
	}

	body, err := marshalDefinition(definition)
	if err != nil {
		return ProviderOutput{}, nil, nil, nil, err
	}
	return ProviderOutput{Slug: slug, YAML: body}, converted, partial, skipped, nil
}

// buildTool converts one Membrane action record's fields into a Beecon
// provider tool: slug/name/description come straight from the action's own
// key/name/inputSchema.description, inputSchema/outputSchema are copied
// through verbatim, and the path/method/query mapping is translated per the
// spec's DSL rules — a simple `$var` shape, a single-fallback `$case`, and a
// `$firstNotEmpty` inputSchema default all translate; anything else is
// reported, never silently emitted as a literal `$`-string. The returned
// caveats (empty for a clean conversion) are the report's Partial-section
// content for this tool; an error means the tool could not be converted at
// all (a SkippedItem).
func buildTool(fields map[string]any) (outputToolV1, []string, error) {
	slug := stringAt(fields, "key")
	if slug == "" {
		return outputToolV1{}, nil, fmt.Errorf("action record missing %q", "key")
	}

	inputSchema := mapAt(fields, "inputSchema")
	if len(inputSchema) == 0 {
		return outputToolV1{}, nil, fmt.Errorf("action %q missing inputSchema", slug)
	}
	outputSchema := mapAt(fields, "customOutputSchema")
	if len(outputSchema) == 0 {
		return outputToolV1{}, nil, fmt.Errorf("action %q missing customOutputSchema", slug)
	}

	request := mapAt(fields, "config", "request")
	method := stringAt(request, "method")
	if method == "" {
		return outputToolV1{}, nil, fmt.Errorf("action %q missing config.request.method", slug)
	}

	path, pathCaveats, pathDefaults, skipReason := resolveToolPath(request)
	if skipReason != "" {
		return outputToolV1{}, nil, fmt.Errorf("%s", skipReason)
	}

	query, queryCaveats, queryDefaults := translateValueMap("query", mapAt(request, "query"))

	caveats := append(pathCaveats, queryCaveats...)
	applyInputSchemaDefaults(inputSchema, append(pathDefaults, queryDefaults...))

	return outputToolV1{
		Slug:         slug,
		Name:         stringAt(fields, "name"),
		Description:  stringAt(inputSchema, "description"),
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mapping: outputToolMappingV1{
			Path:   path,
			Method: method,
			Query:  query,
		},
	}, caveats, nil
}

// applyInputSchemaDefaults sets each schemaDefault's value as the named
// top-level inputSchema property's `default` (Gap C), mutating the schema
// map in place. A default naming a property the schema does not declare is
// silently skipped — it has nothing to attach to.
func applyInputSchemaDefaults(inputSchema map[string]any, defaults []schemaDefault) {
	properties := mapAt(inputSchema, "properties")
	for _, d := range defaults {
		if prop, ok := properties[d.Name].(map[string]any); ok {
			prop["default"] = d.Value
		}
	}
}

func marshalDefinition(definition outputDefinitionV1) ([]byte, error) {
	body, err := yaml.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("marshal provider definition %s: %w", definition.Slug, err)
	}
	return append([]byte(scaffoldBanner), body...), nil
}
