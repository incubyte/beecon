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
	"net/url"

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

// paramFieldData is one field params.gohtml renders (Slice 3, AC3-AC5): Value
// pre-fills a non-secret field with whatever was submitted on a previous,
// failed attempt (never for a Secret field — its input always starts blank);
// Error carries an inline "this field is required" message when this exact
// field failed AC4's validation.
type paramFieldData struct {
	Name        string
	DisplayName string
	Description string
	Required    bool
	Secret      bool
	Value       string
	Error       string
}

// paramsFormData is what params.gohtml renders: the provider's name/logo,
// the token identifying which connection attempt the form submits against,
// and every expected param field.
type paramsFormData struct {
	ProviderName string
	ProviderLogo string
	Token        string
	Fields       []paramFieldData
}

// ConnectPage handles GET /connect/{token} (AC1, AC2, AC3): the provider's
// param-collection form when the definition declares expected params and
// none have been submitted yet, the provider's connect page for a valid,
// unexpired, not-yet-completed connect token otherwise, or an error page —
// never a forward to the provider — when the token itself is invalid.
func (h *Handler) ConnectPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	view, err := h.facade.OpenConnectPage(r.Context(), token)
	if err != nil {
		h.renderError(w, err)
		return
	}
	if view.ParamsRequired {
		h.renderParamsForm(w, token, view, nil, nil)
		return
	}
	h.render(w, http.StatusOK, "connect.gohtml", connectPageData{
		ProviderName: view.ProviderName,
		ProviderLogo: view.ProviderLogo,
		AuthorizeURL: view.AuthorizeURL,
	})
}

// SubmitParams handles POST /connect/{token}/params (Slice 3, AC3, AC4, AC7):
// a submission missing a required field re-renders the form with each such
// field marked invalid and never forwards to the provider (AC4); a valid
// submission stores the values vault-encrypted (AC7) and renders the
// provider's connect page, exactly as OpenConnectPage would once params are
// collected.
func (h *Handler) SubmitParams(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := r.ParseForm(); err != nil {
		h.renderError(w, err)
		return
	}
	values := formValues(r.PostForm)

	view, err := h.facade.SubmitParams(r.Context(), token, values)
	if err != nil {
		if missing, ok := connections.MissingParamFields(err); ok {
			h.renderParamsForm(w, token, view, values, missing)
			return
		}
		h.renderError(w, err)
		return
	}
	h.render(w, http.StatusOK, "connect.gohtml", connectPageData{
		ProviderName: view.ProviderName,
		ProviderLogo: view.ProviderLogo,
		AuthorizeURL: view.AuthorizeURL,
	})
}

// renderParamsForm renders params.gohtml for view's expected-param fields,
// pre-filling non-secret fields with values from a previous submission (nil
// on the initial GET) and marking every name present in missing invalid.
func (h *Handler) renderParamsForm(w http.ResponseWriter, token string, view connections.ConnectPageView, values map[string]string, missing []string) {
	missingSet := toSet(missing)
	fields := make([]paramFieldData, 0, len(view.ParamFields))
	for _, field := range view.ParamFields {
		fields = append(fields, paramFieldData{
			Name:        field.Name,
			DisplayName: field.DisplayName,
			Description: field.Description,
			Required:    field.Required,
			Secret:      field.Secret,
			Value:       fieldValue(field.Secret, field.Name, values),
			Error:       fieldError(field.Name, missingSet),
		})
	}
	h.render(w, http.StatusOK, "params.gohtml", paramsFormData{
		ProviderName: view.ProviderName,
		ProviderLogo: view.ProviderLogo,
		Token:        token,
		Fields:       fields,
	})
}

// formValues flattens a parsed form into a plain map, the shape
// connections.Facade.SubmitParams accepts.
func formValues(form url.Values) map[string]string {
	values := make(map[string]string, len(form))
	for key := range form {
		values[key] = form.Get(key)
	}
	return values
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// fieldValue never echoes a secret field's value back into the form (AC5),
// even on a re-render after a validation failure.
func fieldValue(secret bool, name string, values map[string]string) string {
	if secret || values == nil {
		return ""
	}
	return values[name]
}

func fieldError(name string, missing map[string]bool) string {
	if missing[name] {
		return "This field is required."
	}
	return ""
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
