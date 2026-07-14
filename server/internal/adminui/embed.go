// Package adminui is the Admin UI's driving adapter (PD47): it embeds the
// SPA's built static assets and serves them under /admin. It is a peer of
// connectweb — a thin static-file adapter that imports no domain module; the
// SPA it serves is a pure /api/v1 HTTP client with no server-side logic of
// its own.
package adminui

import "embed"

// distFS embeds every file the Admin UI's build produces under dist/ (the
// FD2 pipeline copies apps/admin-ui's Vite build output here before
// `go build`). "all:" also embeds dotfiles (e.g. dist/.gitkeep) so the
// directive never fails to compile on a clean checkout that hasn't run
// `make build-ui` yet — the committed placeholder dist/index.html is enough
// for a real, non-empty embed either way.
//
//go:embed all:dist
var distFS embed.FS
