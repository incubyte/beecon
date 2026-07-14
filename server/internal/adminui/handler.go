package adminui

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// Handler serves the embedded Admin UI static assets under /admin (PD47):
// hashed build assets are served directly by their own path; any other
// path under /admin falls back to index.html so the SPA's own client-side
// router (basepath '/admin') owns everything below the mount, including a
// hard reload on a deep link like /admin/organizations.
func Handler() (http.Handler, error) {
	assets, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(assets))
	withFallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isEmbeddedFile(assets, r.URL.Path) {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Deliberately not delegated to fileServer with the path rewritten
		// to "/index.html": net/http's FileServer treats a request whose
		// resolved name is literally "index.html" as needing canonicalizing
		// and 301-redirects it to "./" — exactly wrong for a client-side
		// route like /admin/organizations, which must render the SPA shell
		// in place, not bounce the browser. Serving the embedded file's
		// bytes directly sidesteps that redirect entirely.
		serveIndex(w, assets)
	})
	return http.StripPrefix("/admin", withFallback), nil
}

// isEmbeddedFile reports whether requestPath names a real, non-directory
// file in assets — the SPA-fallback boundary: a hit (a hashed JS/CSS asset,
// or the root path itself) is served as-is by http.FileServer; a miss (any
// client-side route) falls back to index.html instead of a 404.
func isEmbeddedFile(assets fs.FS, requestPath string) bool {
	name := strings.TrimPrefix(requestPath, "/")
	if name == "" {
		return true
	}
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

func serveIndex(w http.ResponseWriter, assets fs.FS) {
	index, err := assets.Open("index.html")
	if err != nil {
		http.Error(w, "admin ui not built", http.StatusInternalServerError)
		return
	}
	defer func() { _ = index.Close() }()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(w, index)
}
