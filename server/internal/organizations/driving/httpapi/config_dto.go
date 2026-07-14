package httpapi

import "beecon/internal/organizations"

// configGovernanceDTO is configDocumentDTO's governance section (Slice 9,
// PD46) — the same flattened shape (allowList/hidden/featured/featuredCap)
// as the domain's own ConfigGovernance, deliberately independent of
// governanceDTO's nested "onboarding" object: the export/import wire format
// is its own shape, not GET/PUT .../governance's.
type configGovernanceDTO struct {
	AllowList   *[]string `json:"allowList"`
	Hidden      []string  `json:"hidden"`
	Featured    []string  `json:"featured"`
	FeaturedCap int       `json:"featuredCap"`
}

func toConfigGovernanceDTO(governance organizations.ConfigGovernance) configGovernanceDTO {
	return configGovernanceDTO{
		AllowList:   governance.AllowList,
		Hidden:      nonNilStrings(governance.Hidden),
		Featured:    nonNilStrings(governance.Featured),
		FeaturedCap: governance.FeaturedCap,
	}
}

func (dto configGovernanceDTO) toDomain() organizations.ConfigGovernance {
	return organizations.ConfigGovernance{
		AllowList:   dto.AllowList,
		Hidden:      dto.Hidden,
		Featured:    dto.Featured,
		FeaturedCap: dto.FeaturedCap,
	}
}

// configEndpointDTO is one entry in configDocumentDTO's endpoints section
// (Slice 9, PD46): URL and its event-type filter only — never a secret.
type configEndpointDTO struct {
	URL        string    `json:"url"`
	EventTypes *[]string `json:"eventTypes"`
}

func toConfigEndpointDTO(endpoint organizations.ConfigEndpoint) configEndpointDTO {
	return configEndpointDTO{URL: endpoint.URL, EventTypes: configEventTypesPointer(endpoint.EventTypes)}
}

func (dto configEndpointDTO) toDomain() organizations.ConfigEndpoint {
	return organizations.ConfigEndpoint{URL: dto.URL, EventTypes: dto.eventTypes()}
}

func (dto configEndpointDTO) eventTypes() []string {
	if dto.EventTypes == nil {
		return nil
	}
	return *dto.EventTypes
}

// configEventTypesPointer renders a nil EventTypes filter as JSON null
// (match every event type) rather than an empty array — mirrors delivery's
// own driving/httpapi eventTypesPointer helper; organizations cannot import
// it (BOUNDARIES), so it is re-declared here, under its own name, at the
// one call site this package needs it for.
func configEventTypesPointer(types []string) *[]string {
	if types == nil {
		return nil
	}
	return &types
}

// configRetentionDTO is configDocumentDTO's retention section (Slice 9,
// PD46) — the same tri-state pointers as retentionDTO's own LogDays/
// EventDays, named to match the export/import wire format's own field
// names.
type configRetentionDTO struct {
	LogRetentionDays   *int `json:"logRetentionDays"`
	EventRetentionDays *int `json:"eventRetentionDays"`
}

func toConfigRetentionDTO(retention organizations.ConfigRetention) configRetentionDTO {
	return configRetentionDTO{LogRetentionDays: retention.LogRetentionDays, EventRetentionDays: retention.EventRetentionDays}
}

func (dto configRetentionDTO) toDomain() organizations.ConfigRetention {
	return organizations.ConfigRetention{LogRetentionDays: dto.LogRetentionDays, EventRetentionDays: dto.EventRetentionDays}
}

// configDocumentDTO is GET .../config/export's response and POST
// .../config/import's request body (Slice 9, PD46, API Shape): the
// versioned document itself — never a secret/credential/connection/
// user-token/provider-definition field exists on this type.
type configDocumentDTO struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Governance    configGovernanceDTO `json:"governance"`
	Endpoints     []configEndpointDTO `json:"endpoints"`
	Retention     configRetentionDTO  `json:"retention"`
}

func toConfigDocumentDTO(document organizations.ConfigDocument) configDocumentDTO {
	endpoints := make([]configEndpointDTO, 0, len(document.Endpoints))
	for _, endpoint := range document.Endpoints {
		endpoints = append(endpoints, toConfigEndpointDTO(endpoint))
	}
	return configDocumentDTO{
		SchemaVersion: document.SchemaVersion,
		Governance:    toConfigGovernanceDTO(document.Governance),
		Endpoints:     endpoints,
		Retention:     toConfigRetentionDTO(document.Retention),
	}
}

func (dto configDocumentDTO) toDomain() organizations.ConfigDocument {
	endpoints := make([]organizations.ConfigEndpoint, 0, len(dto.Endpoints))
	for _, endpoint := range dto.Endpoints {
		endpoints = append(endpoints, endpoint.toDomain())
	}
	return organizations.ConfigDocument{
		SchemaVersion: dto.SchemaVersion,
		Governance:    dto.Governance.toDomain(),
		Endpoints:     endpoints,
		Retention:     dto.Retention.toDomain(),
	}
}

// configChangeDTO is one line of importPlanDTO's/importApplyDTO's own array
// (Slice 9, PD46).
type configChangeDTO struct {
	Area   string `json:"area"`
	Field  string `json:"field"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

func toConfigChangeDTOs(changes []organizations.ConfigChange) []configChangeDTO {
	dtos := make([]configChangeDTO, 0, len(changes))
	for _, change := range changes {
		dtos = append(dtos, configChangeDTO{Area: change.Area, Field: change.Field, Action: change.Action, Detail: change.Detail})
	}
	return dtos
}

// importedSecretDTO is one entry in an apply's secrets array (Slice 9,
// PD46): the freshly minted signing secret for an endpoint the import
// created, shown exactly once.
type importedSecretDTO struct {
	EndpointID string `json:"wepId"`
	Secret     string `json:"secret"`
}

func toImportedSecretDTOs(secrets []organizations.ImportedEndpointSecret) []importedSecretDTO {
	dtos := make([]importedSecretDTO, 0, len(secrets))
	for _, secret := range secrets {
		dtos = append(dtos, importedSecretDTO{EndpointID: secret.EndpointID, Secret: secret.Secret})
	}
	return dtos
}

// importPlanDTO is a dry-run import's response (Slice 9, PD46, API Shape):
// nothing was written.
type importPlanDTO struct {
	Plan     []configChangeDTO `json:"plan"`
	Warnings []string          `json:"warnings"`
}

func toImportPlanDTO(result organizations.ImportResult) importPlanDTO {
	return importPlanDTO{Plan: toConfigChangeDTOs(result.Plan), Warnings: nonNilConfigWarnings(result.Warnings)}
}

func nonNilConfigWarnings(warnings []string) []string {
	if warnings == nil {
		return []string{}
	}
	return warnings
}

// importApplyDTO is a non-dry-run import's response (Slice 9, PD46, API
// Shape): what was applied, plus any freshly minted endpoint secrets.
type importApplyDTO struct {
	Applied []configChangeDTO   `json:"applied"`
	Secrets []importedSecretDTO `json:"secrets"`
}

func toImportApplyDTO(result organizations.ImportResult) importApplyDTO {
	return importApplyDTO{Applied: toConfigChangeDTOs(result.Applied), Secrets: toImportedSecretDTOs(result.Secrets)}
}
