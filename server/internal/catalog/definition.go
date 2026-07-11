package catalog

import (
	"fmt"
	"io/fs"
	"sort"

	"gopkg.in/yaml.v3"
)

// providerDefinitionFile is the on-disk YAML shape of one provider
// definition file (AC1: name, logo, OAuth authorize/token endpoints, scopes,
// tool definitions).
type providerDefinitionFile struct {
	Slug       string             `yaml:"slug"`
	Name       string             `yaml:"name"`
	Logo       string             `yaml:"logo"`
	AuthScheme string             `yaml:"authScheme"`
	OAuth      oauthConfigFile    `yaml:"oauth"`
	Tools      []providerToolFile `yaml:"tools"`
}

type oauthConfigFile struct {
	AuthorizeURL string   `yaml:"authorizeUrl"`
	TokenURL     string   `yaml:"tokenUrl"`
	UserInfoURL  string   `yaml:"userInfoUrl"`
	Scopes       []string `yaml:"scopes"`
}

type providerToolFile struct {
	Slug        string         `yaml:"slug"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Method      string         `yaml:"method"`
	Path        string         `yaml:"path"`
	InputSchema map[string]any `yaml:"inputSchema"`
}

// LoadProviderDefinitions reads and validates every *.yaml file directly
// under fsys, in deterministic (sorted by filename) order. It fails with an
// error naming the file and the invalid field (AC2) rather than returning a
// partially loaded provider.
func LoadProviderDefinitions(fsys fs.FS) ([]ProviderDefinition, error) {
	names, err := yamlFileNames(fsys)
	if err != nil {
		return nil, err
	}

	definitions := make([]ProviderDefinition, 0, len(names))
	for _, name := range names {
		definition, err := loadProviderDefinitionFile(fsys, name)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func yamlFileNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read provider definitions directory: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func loadProviderDefinitionFile(fsys fs.FS, name string) (ProviderDefinition, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return ProviderDefinition{}, fmt.Errorf("read provider definition %s: %w", name, err)
	}

	var file providerDefinitionFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return ProviderDefinition{}, fmt.Errorf("parse provider definition %s: %w", name, err)
	}

	if err := validateProviderDefinitionFile(name, file); err != nil {
		return ProviderDefinition{}, err
	}

	return providerDefinitionFromFile(file), nil
}

// validateProviderDefinitionFile checks every field AC1 requires a provider
// definition to carry, returning a *definitionError naming both name (the
// file) and the missing/invalid field (AC2).
func validateProviderDefinitionFile(name string, file providerDefinitionFile) error {
	required := []struct {
		field string
		value string
	}{
		{"slug", file.Slug},
		{"name", file.Name},
		{"logo", file.Logo},
		{"oauth.authorizeUrl", file.OAuth.AuthorizeURL},
		{"oauth.tokenUrl", file.OAuth.TokenURL},
	}
	for _, r := range required {
		if r.value == "" {
			return definitionError(name, r.field, "must not be empty")
		}
	}
	if len(file.OAuth.Scopes) == 0 {
		return definitionError(name, "oauth.scopes", "must declare at least one scope")
	}
	for i, tool := range file.Tools {
		if err := validateProviderToolFile(name, i, tool); err != nil {
			return err
		}
	}
	return nil
}

func validateProviderToolFile(fileName string, index int, tool providerToolFile) error {
	if tool.Slug == "" {
		return definitionError(fileName, fmt.Sprintf("tools[%d].slug", index), "must not be empty")
	}
	if tool.Method == "" {
		return definitionError(fileName, fmt.Sprintf("tools[%d].method", index), "must not be empty")
	}
	if tool.Path == "" {
		return definitionError(fileName, fmt.Sprintf("tools[%d].path", index), "must not be empty")
	}
	return nil
}

func definitionError(fileName, field, issue string) error {
	return fmt.Errorf("invalid provider definition %s: field %q %s", fileName, field, issue)
}

func providerDefinitionFromFile(file providerDefinitionFile) ProviderDefinition {
	authScheme := file.AuthScheme
	if authScheme == "" {
		authScheme = "oauth2"
	}
	return ProviderDefinition{
		Slug:         file.Slug,
		Name:         file.Name,
		Logo:         file.Logo,
		AuthScheme:   authScheme,
		AuthorizeURL: file.OAuth.AuthorizeURL,
		TokenURL:     file.OAuth.TokenURL,
		UserInfoURL:  file.OAuth.UserInfoURL,
		Scopes:       file.OAuth.Scopes,
		Tools:        toolsFromFile(file.Tools),
	}
}

func toolsFromFile(files []providerToolFile) []ProviderTool {
	tools := make([]ProviderTool, 0, len(files))
	for _, f := range files {
		tools = append(tools, ProviderTool{
			Slug:        f.Slug,
			Name:        f.Name,
			Description: f.Description,
			Method:      f.Method,
			Path:        f.Path,
			InputSchema: f.InputSchema,
		})
	}
	return tools
}
