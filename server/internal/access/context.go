package access

import "context"

// contextKey is deliberately unexported so no other package can collide with
// or forge this context value.
type contextKey int

const scopeContextKey contextKey = iota

// WithScope returns a context carrying scope. Called by
// authmw.OrgAuth once it has verified an org-scoped API key (PD41, Slice
// 4) — access owns Scope, so it also owns this context-key helper, the same
// reasoning organizations.WithOrgID documents for OrgID: keeping the scope
// concept out of organizations (BOUNDARIES: scope is an org-key concept
// only) means organizations never learns about it.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey, scope)
}

// ScopeFromContext returns the Scope that OrgAuth injected into ctx. ok is
// false for a request that never authenticated through an org API key (an
// admin-key or user-token request) — authmw.RequireWrite treats that as
// "no scope to restrict", since scope is an org-key concept only.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeContextKey).(Scope)
	return scope, ok
}
