package organizations

import "context"

// contextKey is deliberately unexported so no other package can collide with
// or forge this context value.
type contextKey int

const orgIDContextKey contextKey = iota

// WithOrgID returns a context carrying org. Called by authentication
// middleware (access.driving.authmw.OrgAuth) once it has verified an
// org-scoped API key — organizations owns OrgID, so it also owns the
// context-key helpers, keeping every other module's handlers free of an
// import on the access module just to read the authenticated organization.
func WithOrgID(ctx context.Context, org OrgID) context.Context {
	return context.WithValue(ctx, orgIDContextKey, org)
}

// OrgIDFromContext returns the OrgID that authentication middleware injected
// into ctx. ok is false if the request never passed through org-scoped
// authentication.
func OrgIDFromContext(ctx context.Context) (OrgID, bool) {
	org, ok := ctx.Value(orgIDContextKey).(OrgID)
	return org, ok
}
