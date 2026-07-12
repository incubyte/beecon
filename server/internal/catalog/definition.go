package catalog

import (
	"fmt"
	"io/fs"
	"sort"

	"gopkg.in/yaml.v3"
)

// supportedFormatVersion is the only provider definition format version this
// build understands. Definitions are embedded in the binary — no deployed
// user data exists in an older format — so there is deliberately no
// dual-format support: a future v2 adds a parser and a dispatch arm below,
// nothing else changes.
const supportedFormatVersion = 1

// formatVersionPeek decodes only the field every provider definition file
// must carry so LoadProviderDefinitions can dispatch to the right version's
// parser before attempting to parse anything else.
type formatVersionPeek struct {
	FormatVersion int `yaml:"formatVersion"`
}

// LoadProviderDefinitions reads and validates every *.yaml file directly
// under fsys, in deterministic (sorted by filename) order. It fails with an
// error naming the file and the invalid field rather than returning a
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

// loadProviderDefinitionFile peeks the file's formatVersion and dispatches to
// that version's parser. A missing or unsupported formatVersion fails boot
// naming the file, the version found (0 for missing), and the version this
// build supports.
func loadProviderDefinitionFile(fsys fs.FS, name string) (ProviderDefinition, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return ProviderDefinition{}, fmt.Errorf("read provider definition %s: %w", name, err)
	}

	version, err := peekFormatVersion(name, raw)
	if err != nil {
		return ProviderDefinition{}, err
	}

	switch version {
	case 1:
		return loadProviderDefinitionFileV1(name, raw)
	default:
		return ProviderDefinition{}, unsupportedFormatVersionError(name, version)
	}
}

func peekFormatVersion(name string, raw []byte) (int, error) {
	var peek formatVersionPeek
	if err := yaml.Unmarshal(raw, &peek); err != nil {
		return 0, fmt.Errorf("parse provider definition %s: %w", name, err)
	}
	if peek.FormatVersion == 0 {
		return 0, unsupportedFormatVersionError(name, 0)
	}
	return peek.FormatVersion, nil
}

func unsupportedFormatVersionError(name string, found int) error {
	return fmt.Errorf(
		"invalid provider definition %s: formatVersion %d is not supported (supported: %d)",
		name, found, supportedFormatVersion,
	)
}

func definitionError(fileName, field, issue string) error {
	return fmt.Errorf("invalid provider definition %s: field %q %s", fileName, field, issue)
}
