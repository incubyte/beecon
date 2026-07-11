package connectweb

import (
	"embed"
	"html/template"
)

//go:embed templates/*.gohtml
var templateFiles embed.FS

// parseTemplates parses every embedded template so the connect flow ships
// inside the single binary (no on-disk template directory required at
// runtime).
func parseTemplates() (*template.Template, error) {
	return template.ParseFS(templateFiles, "templates/*.gohtml")
}
