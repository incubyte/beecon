package catalog

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// definitionFileV1 is the on-disk YAML shape of one provider definition file
// under the finalized format (PD13, formatVersion: 1): provider identity,
// OAuth endpoints/scopes, the provider-level mapping (today just baseUrl),
// and the tool list. Triggers is a reserved key (Phase 3) accepted and left
// unvalidated so a Phase 3 definition can land in files before the code that
// interprets it does.
type definitionFileV1 struct {
	FormatVersion  int                   `yaml:"formatVersion"`
	Slug           string                `yaml:"slug"`
	Name           string                `yaml:"name"`
	Logo           string                `yaml:"logo"`
	AuthScheme     string                `yaml:"authScheme"`
	OAuth          oauthConfigFileV1     `yaml:"oauth"`
	Mapping        providerMappingFileV1 `yaml:"mapping"`
	ExpectedParams []expectedParamFileV1 `yaml:"expectedParams"`
	Tools          []providerToolFileV1  `yaml:"tools"`
	Triggers       any                   `yaml:"triggers"`
}

// expectedParamFileV1 is PD13's expectedParams shape (Slice 3): one pre-auth
// value the end user must supply before OAuth can start.
type expectedParamFileV1 struct {
	Name        string `yaml:"name"`
	DisplayName string `yaml:"displayName"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Secret      bool   `yaml:"secret"`
}

type oauthConfigFileV1 struct {
	AuthorizeURL    string         `yaml:"authorizeUrl"`
	TokenURL        string         `yaml:"tokenUrl"`
	UserInfoURL     string         `yaml:"userInfoUrl"`
	Scopes          []string       `yaml:"scopes"`
	CredentialStyle string         `yaml:"credentialStyle"`
	UserInfo        userInfoFileV1 `yaml:"userInfo"`
}

// userInfoFileV1 is PD13's userInfo field mapping: which field of the
// provider's user-info/token-metadata response names the account's email and
// display name (Outlook: mail/displayName; Hubspot: user/hub_domain, PD16).
type userInfoFileV1 struct {
	Email       string `yaml:"email"`
	DisplayName string `yaml:"displayName"`
}

// providerMappingFileV1 is the provider-level half of PD13's mapping block:
// the base URL every tool's path is relative to.
type providerMappingFileV1 struct {
	BaseURL string `yaml:"baseUrl"`
}

type providerToolFileV1 struct {
	Slug         string            `yaml:"slug"`
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Deprecated   bool              `yaml:"deprecated"`
	InputSchema  map[string]any    `yaml:"inputSchema"`
	OutputSchema map[string]any    `yaml:"outputSchema"`
	Mapping      toolMappingFileV1 `yaml:"mapping"`
}

// toolMappingFileV1 is the tool-level half of PD13's mapping block: the path
// (relative to the provider's baseUrl, {input.x}/{params.x} templated) and
// method every call uses, plus optional query/header/body mapping,
// pagination declaration, and file-typed inputs.
type toolMappingFileV1 struct {
	Path       string            `yaml:"path"`
	Method     string            `yaml:"method"`
	Query      map[string]string `yaml:"query"`
	Header     map[string]string `yaml:"header"`
	Body       map[string]string `yaml:"body"`
	Pagination *paginationFileV1 `yaml:"pagination"`
	FileInputs []string          `yaml:"fileInputs"`
}

type paginationFileV1 struct {
	PageSizeParam  string `yaml:"pageSizeParam"`
	CursorParam    string `yaml:"cursorParam"`
	NextCursorPath string `yaml:"nextCursorPath"`
}

// loadProviderDefinitionFileV1 parses raw as the finalized definition format
// (strict/KnownFields decoding, so a typo'd field fails boot instead of
// silently no-op'ing), validates it field-precisely, and converts it into
// the domain ProviderDefinition.
func loadProviderDefinitionFileV1(name string, raw []byte) (ProviderDefinition, error) {
	var file definitionFileV1
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return ProviderDefinition{}, fmt.Errorf("parse provider definition %s: %w", name, err)
	}
	if err := validateDefinitionFileV1(name, file); err != nil {
		return ProviderDefinition{}, err
	}
	return definitionFromFileV1(file), nil
}

// validateDefinitionFileV1 checks every field the finalized format requires
// a provider definition to carry, returning a field-path-precise error (file
// + field + issue) naming the first problem found.
func validateDefinitionFileV1(name string, file definitionFileV1) error {
	required := []struct {
		field string
		value string
	}{
		{"slug", file.Slug},
		{"name", file.Name},
		{"logo", file.Logo},
		{"oauth.authorizeUrl", file.OAuth.AuthorizeURL},
		{"oauth.tokenUrl", file.OAuth.TokenURL},
		{"mapping.baseUrl", file.Mapping.BaseURL},
	}
	for _, r := range required {
		if r.value == "" {
			return definitionError(name, r.field, "must not be empty")
		}
	}
	if len(file.OAuth.Scopes) == 0 {
		return definitionError(name, "oauth.scopes", "must declare at least one scope")
	}
	if err := validateCredentialStyle(name, file.OAuth.CredentialStyle); err != nil {
		return err
	}
	for i, tool := range file.Tools {
		if err := validateProviderToolFileV1(name, i, tool); err != nil {
			return err
		}
	}
	for i, param := range file.ExpectedParams {
		if err := validateExpectedParamFileV1(name, i, param); err != nil {
			return err
		}
	}
	return nil
}

// validateExpectedParamFileV1 checks one expectedParams entry (Slice 3, AC1):
// name is what {params.x} templating and the stored values map address the
// value by, and displayName is what the connect page's form labels it with —
// both must be present for the field to mean anything.
func validateExpectedParamFileV1(fileName string, index int, param expectedParamFileV1) error {
	prefix := fmt.Sprintf("expectedParams[%d]", index)
	if param.Name == "" {
		return definitionError(fileName, prefix+".name", "must not be empty")
	}
	if param.DisplayName == "" {
		return definitionError(fileName, prefix+".displayName", "must not be empty")
	}
	return nil
}

// validateCredentialStyle accepts an omitted style (defaulted to
// CredentialStyleFormBody by definitionFromFileV1) or either declared enum
// value, and rejects anything else field-precisely.
func validateCredentialStyle(fileName, style string) error {
	switch style {
	case "", CredentialStyleFormBody, CredentialStyleBasicAuth:
		return nil
	default:
		return definitionError(fileName, "oauth.credentialStyle", `must be "formBody" or "basicAuth"`)
	}
}

func validateProviderToolFileV1(fileName string, index int, tool providerToolFileV1) error {
	prefix := fmt.Sprintf("tools[%d]", index)
	if tool.Slug == "" {
		return definitionError(fileName, prefix+".slug", "must not be empty")
	}
	if tool.Mapping.Method == "" {
		return definitionError(fileName, prefix+".mapping.method", "must not be empty")
	}
	if tool.Mapping.Path == "" {
		return definitionError(fileName, prefix+".mapping.path", "must not be empty")
	}
	if len(tool.InputSchema) == 0 {
		return definitionError(fileName, prefix+".inputSchema", "must not be empty")
	}
	if len(tool.OutputSchema) == 0 {
		return definitionError(fileName, prefix+".outputSchema", "must not be empty")
	}
	return nil
}

func definitionFromFileV1(file definitionFileV1) ProviderDefinition {
	authScheme := file.AuthScheme
	if authScheme == "" {
		authScheme = "oauth2"
	}
	credentialStyle := file.OAuth.CredentialStyle
	if credentialStyle == "" {
		credentialStyle = CredentialStyleFormBody
	}
	return ProviderDefinition{
		Slug:            file.Slug,
		Name:            file.Name,
		Logo:            file.Logo,
		AuthScheme:      authScheme,
		BaseURL:         file.Mapping.BaseURL,
		AuthorizeURL:    file.OAuth.AuthorizeURL,
		TokenURL:        file.OAuth.TokenURL,
		UserInfoURL:     file.OAuth.UserInfoURL,
		Scopes:          file.OAuth.Scopes,
		CredentialStyle: credentialStyle,
		UserInfo: UserInfoMapping{
			EmailField:       file.OAuth.UserInfo.Email,
			DisplayNameField: file.OAuth.UserInfo.DisplayName,
		},
		ExpectedParams: expectedParamsFromFileV1(file.ExpectedParams),
		Tools:          toolsFromFileV1(file.Tools),
	}
}

func expectedParamsFromFileV1(files []expectedParamFileV1) []ExpectedParam {
	params := make([]ExpectedParam, 0, len(files))
	for _, f := range files {
		params = append(params, ExpectedParam{
			Name:        f.Name,
			DisplayName: f.DisplayName,
			Description: f.Description,
			Required:    f.Required,
			Secret:      f.Secret,
		})
	}
	return params
}

func toolsFromFileV1(files []providerToolFileV1) []ProviderTool {
	tools := make([]ProviderTool, 0, len(files))
	for _, f := range files {
		tools = append(tools, ProviderTool{
			Slug:         f.Slug,
			Name:         f.Name,
			Description:  f.Description,
			Method:       f.Mapping.Method,
			Path:         f.Mapping.Path,
			InputSchema:  f.InputSchema,
			OutputSchema: f.OutputSchema,
			Deprecated:   f.Deprecated,
			Mapping:      mappingFromFileV1(f.Mapping),
		})
	}
	return tools
}

func mappingFromFileV1(m toolMappingFileV1) Mapping {
	return Mapping{
		Query:      m.Query,
		Header:     m.Header,
		Body:       m.Body,
		Pagination: paginationFromFileV1(m.Pagination),
		FileInputs: m.FileInputs,
	}
}

func paginationFromFileV1(p *paginationFileV1) *Pagination {
	if p == nil {
		return nil
	}
	return &Pagination{
		PageSizeParam:  p.PageSizeParam,
		CursorParam:    p.CursorParam,
		NextCursorPath: p.NextCursorPath,
	}
}
