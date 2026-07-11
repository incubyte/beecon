// Package connectweb is the connections module's HTML driving adapter: the
// middle-man connect page and OAuth callback the end user's browser visits
// directly. Neither route is authenticated by an organization API key —
// the single-use connect token (and, from the callback on, the CSRF state)
// is the credential. Templates are embedded so the connect flow ships inside
// the single binary (Slice 4).
package connectweb

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"

	"beecon/internal/connections"
	"beecon/internal/httpx"
)

// Handler serves GET /connect/{token} and GET /connect/oauth/callback.
type Handler struct {
	facade    *connections.Facade
	templates *template.Template
}

// NewHandler parses the embedded templates once at wiring time, so a
// malformed template fails fast at boot rather than on the first request.
func NewHandler(facade *connections.Facade) (*Handler, error) {
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	return &Handler{facade: facade, templates: templates}, nil
}

// connectPageData is what connect.gohtml renders (AC1): the provider's name
// and logo, and the Microsoft consent link the Connect action points at
// (AC3).
type connectPageData struct {
	ProviderName string
	ProviderLogo string
	AuthorizeURL string
}

// errorPageData is what error.gohtml renders (AC2, AC7, AC9): a human
// message, and never a redirect to the provider.
type errorPageData struct {
	Message string
}

// ConnectPage handles GET /connect/{token} (AC1, AC2, AC3): the provider's
// connect page for a valid, unexpired, not-yet-completed connect token, or
// an error page — never a forward to the provider — otherwise.
func (h *Handler) ConnectPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	view, err := h.facade.OpenConnectPage(r.Context(), token)
	if err != nil {
		h.renderError(w, err)
		return
	}
	h.render(w, http.StatusOK, "connect.gohtml", connectPageData{
		ProviderName: view.ProviderName,
		ProviderLogo: view.ProviderLogo,
		AuthorizeURL: view.AuthorizeURL,
	})
}

// Callback handles GET /connect/oauth/callback (AC4, AC7, AC8, AC9): a valid
// CSRF state redirects the browser back to the consumer's redirectUri,
// whether the provider granted or denied consent; a missing, unknown,
// expired, or already-used state — or a failed token exchange — renders an
// error page instead, and the connection is left exactly as HandleCallback's
// own rules dictate (PD11).
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	outcome, err := h.facade.HandleCallback(r.Context(), query.Get("code"), query.Get("state"), query.Get("error"))
	if err != nil {
		h.renderError(w, err)
		return
	}
	http.Redirect(w, r, outcome.RedirectURL, http.StatusFound)
}

func (h *Handler) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = h.templates.ExecuteTemplate(w, name, data)
}

// renderError renders error.gohtml with a *httpx.DomainError's own message
// and status when err carries one, or a generic message and 400 otherwise.
func (h *Handler) renderError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	message := "This connection link is no longer valid."
	var domainErr *httpx.DomainError
	if errors.As(err, &domainErr) && domainErr.Message != "" {
		status = domainErr.Status
		message = domainErr.Message
	}
	h.render(w, status, "error.gohtml", errorPageData{Message: message})
}
