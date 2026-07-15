package access

import "context"

// contextKey is deliberately unexported so no other package can collide with
// or forge this context value.
type contextKey int

const (
	scopeContextKey contextKey = iota
	operatorIDContextKey
	operatorSessionIDContextKey
)

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

// WithOperator returns a context carrying the authenticated operator's id.
// Called by authmw.ConsoleAuth/authmw.OperatorSession once a session cookie
// has verified (PD49/PD56) — access owns OperatorID, so it also owns this
// context-key helper, the same reasoning WithScope's own doc comment gives.
func WithOperator(ctx context.Context, id OperatorID) context.Context {
	return context.WithValue(ctx, operatorIDContextKey, id)
}

// OperatorFromContext returns the OperatorID that ConsoleAuth/OperatorSession
// injected into ctx. ok is false for a request that authenticated through
// the break-glass admin key instead of a session — there is no acting
// operator to attribute in that case.
func OperatorFromContext(ctx context.Context) (OperatorID, bool) {
	id, ok := ctx.Value(operatorIDContextKey).(OperatorID)
	return id, ok
}

// WithOperatorSession returns a context carrying the authenticated
// session's own id (Slice 4, carry-forward AC4). Called by
// authmw.ConsoleAuth/authmw.OperatorSession alongside WithOperator, once a
// session cookie has verified: ChangeMyPassword needs to know which of the
// operator's own sessions is the acting one, so RevokeAllForOperatorExcept
// can keep it alive while revoking every other session.
func WithOperatorSession(ctx context.Context, id OperatorSessionID) context.Context {
	return context.WithValue(ctx, operatorSessionIDContextKey, id)
}

// OperatorSessionFromContext returns the OperatorSessionID that
// ConsoleAuth/OperatorSession injected into ctx. ok is false for a request
// that authenticated through the break-glass admin key instead of a session
// — there is no acting session to preserve in that case.
func OperatorSessionFromContext(ctx context.Context) (OperatorSessionID, bool) {
	id, ok := ctx.Value(operatorSessionIDContextKey).(OperatorSessionID)
	return id, ok
}
