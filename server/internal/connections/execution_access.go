// execution_access.go exposes what the execution module (Slice 5) needs from
// a Connection to call a provider on the user's behalf, without ever handing
// a raw token to a DTO or a log line: the vault stays private to this
// module. Slice 4 adds the two halves of PD18's on-demand refresh: an inline
// refresh here when the stored access token has already expired, and a
// forced RefreshForExecution the execution facade calls reactively after a
// provider 401.
package connections

import (
	"context"

	"beecon/internal/organizations"
)

// ExecutionAccess is what a tool execution needs from a Connection: its
// current status (a non-ACTIVE connection is a tool-level failure the caller
// decides how to report, AC4), and — only meaningful when ACTIVE — the
// connection's access token and its collected pre-auth param values,
// decrypted for this one call (Slice 3, AC8: usable via {params.x}
// templating in tool mappings). Neither is ever held longer than the single
// provider call it authorizes, put on a DTO, or written to a log line
// unredacted.
type ExecutionAccess struct {
	Status      Status
	AccessToken string
	Params      map[string]string
}

// ResolveForExecution looks up a Connection scoped to org and confirms it
// belongs to userID. A connection that does not exist, belongs to another
// organization, or does not belong to userID is indistinguishable —
// ErrNotFound, no existence leak (AC5, AC6). A non-ACTIVE connection is
// returned with its status and no access token, letting the caller decide
// how to report that as a tool-level failure (AC4) rather than an HTTP
// error. When the connection is ACTIVE but its stored access token has
// expired (PD18, Slice 4's AC7) — including a Phase 1 row with no recorded
// expiry at all, self-healed on first use — it is refreshed inline before
// the caller ever sees it, so a normal ACTIVE result always carries a
// usable token.
func (f *Facade) ResolveForExecution(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id ConnectionID) (ExecutionAccess, error) {
	connection, err := f.findExecutionConnection(ctx, org, userID, id)
	if err != nil {
		return ExecutionAccess{}, err
	}
	if connection.Status == StatusActive && connection.needsRefresh(f.now()) {
		refreshed, err := f.refreshConnection(ctx, connection)
		if err != nil {
			return ExecutionAccess{}, err
		}
		connection = refreshed
	}
	return f.toExecutionAccess(connection)
}

// RefreshForExecution forces one refresh_token grant for a Connection scoped
// to org and userID, regardless of its stored token_expires_at — the
// reactive half of PD18 (Slice 4, AC7-AC9): the execution facade calls this
// after a provider 401, so the retried call can use a fresh access token. A
// connection that does not exist, belongs to another organization, or does
// not belong to userID is ErrNotFound, matching ResolveForExecution (AC5,
// AC6). A connection that is not ACTIVE is returned as-is (nothing to
// refresh); a refresh the provider rejects surfaces as EXPIRED, not an
// error, so the caller reports it the same way it reports any other
// non-ACTIVE connection (AC9).
func (f *Facade) RefreshForExecution(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id ConnectionID) (ExecutionAccess, error) {
	connection, err := f.findExecutionConnection(ctx, org, userID, id)
	if err != nil {
		return ExecutionAccess{}, err
	}
	if connection.Status != StatusActive {
		return f.toExecutionAccess(connection)
	}
	refreshed, err := f.refreshConnection(ctx, connection)
	if err != nil {
		return ExecutionAccess{}, err
	}
	return f.toExecutionAccess(refreshed)
}

// findExecutionConnection looks up a Connection scoped to org and confirms
// it belongs to userID, sharing ResolveForExecution's and
// RefreshForExecution's identical not-found rule (AC5, AC6).
func (f *Facade) findExecutionConnection(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id ConnectionID) (Connection, error) {
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return Connection{}, err
	}
	if connection == nil || connection.UserID != userID {
		return Connection{}, ErrNotFound()
	}
	return *connection, nil
}

// toExecutionAccess turns a Connection into the ExecutionAccess a tool
// execution needs: no access token or params at all for a non-ACTIVE
// connection (AC4), decrypted for this one call otherwise.
func (f *Facade) toExecutionAccess(connection Connection) (ExecutionAccess, error) {
	if connection.Status != StatusActive {
		return ExecutionAccess{Status: connection.Status}, nil
	}
	accessToken, err := f.vault.Decrypt(connection.EncryptedAccessToken)
	if err != nil {
		return ExecutionAccess{}, err
	}
	params, err := f.decryptParams(connection.EncryptedParams)
	if err != nil {
		return ExecutionAccess{}, err
	}
	return ExecutionAccess{Status: connection.Status, AccessToken: accessToken, Params: params}, nil
}
