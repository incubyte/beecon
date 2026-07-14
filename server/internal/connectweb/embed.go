package connectweb

import (
	"embed"
	"html/template"
)

//go:embed templates/*.gohtml templates/style.css
var templateFiles embed.FS

// parseTemplates parses every embedded template so the connect flow ships
// inside the single binary (no on-disk template directory required at
// runtime).
func parseTemplates() (*template.Template, error) {
	return template.ParseFS(templateFiles, "templates/*.gohtml")
}

// stylesheet reads the shared design-token stylesheet (Slice 10, PD48) the
// three connect templates link, embedded alongside them so it ships inside
// the same single binary.
func stylesheet() ([]byte, error) {
	return templateFiles.ReadFile("templates/style.css")
}
