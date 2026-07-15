package authmw

import (
	"net/http"
	"time"
)

// SessionCookieName and CSRFCookieName are PD52's two cookies: the session
// cookie is HttpOnly (never readable by JS); the CSRF cookie deliberately
// is not — the SPA reads it to echo as the X-CSRF-Token header on mutating
// requests (Slice 3).
const (
	SessionCookieName = "beecon_session"
	CSRFCookieName    = "beecon_csrf"
)

// SetSessionCookies writes both PD52 cookies after a successful login:
// HttpOnly+Secure+SameSite=Strict on the session token, Secure+SameSite=Strict
// (not HttpOnly) on the CSRF token, both expiring alongside the session
// itself. secure is derived from BEECON_BASE_URL's scheme (FD-E) — false
// only lets a non-TLS local/dev deployment exercise cookie-based auth at
// all; every real deployment sets BEECON_BASE_URL to an https:// origin.
func SetSessionCookies(w http.ResponseWriter, token, csrfToken string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookies overwrites both PD52 cookies with an empty value and
// an already-past expiry, so the browser discards them immediately (Slice
// 2's Logout calls this) — defined now, alongside SetSessionCookies, so the
// cookie shape lives in exactly one place for its whole lifecycle.
func ClearSessionCookies(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: CSRFCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

// SessionTokenFromRequest reads the opaque session token from r's
// beecon_session cookie. ok is false when the cookie is absent or empty.
func SessionTokenFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}
