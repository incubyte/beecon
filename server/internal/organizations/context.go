package organizations

import "context"

// contextKey is deliberately unexported so no other package can collide with
// or forge this context value.
type contextKey int

const (
	orgIDContextKey contextKey = iota
	userIDContextKey
)

// WithOrgID returns a context carrying org. Called by authentication
// middleware (access.driving.authmw.OrgAuth, UserAuth, and OrgOrUser) once it
// has verified an org-scoped API key or a user token — organizations owns
// OrgID, so it also owns the context-key helpers, keeping every other
// module's handlers free of an import on the access module just to read the
// authenticated organization.
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

// WithUserID returns a context carrying userID. Called by
// access.driving.authmw.UserAuth (and the OrgOrUser combinator, on its
// user-token branch) once it has verified a user-scoped browser token
// (PD20) — present only when the request authenticated as a specific user,
// never for an org-key request.
func WithUserID(ctx context.Context, userID UserID) context.Context {
	return context.WithValue(ctx, userIDContextKey, userID)
}

// UserIDFromContext returns the UserID that UserAuth (or OrgOrUser)
// injected into ctx. ok is false for a request authenticated by an org API
// key, or one that never passed through user-token authentication at all —
// handlers use this to distinguish "any caller in this org" (org key) from
// "this specific user" (user token) requests.
func UserIDFromContext(ctx context.Context) (UserID, bool) {
	userID, ok := ctx.Value(userIDContextKey).(UserID)
	return userID, ok
}
