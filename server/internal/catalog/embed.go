package catalog

import (
	"embed"
	"io/fs"
)

//go:embed providers/*.yaml
var embeddedProviderFiles embed.FS

// DefaultProviderDefinitions loads and validates every provider definition
// bundled with the binary (AC1). It fails fast (AC2) with an error naming the
// file and field when a definition is invalid.
func DefaultProviderDefinitions() ([]ProviderDefinition, error) {
	fsys, err := fs.Sub(embeddedProviderFiles, "providers")
	if err != nil {
		return nil, err
	}
	return LoadProviderDefinitions(fsys)
}
