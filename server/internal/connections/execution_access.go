// execution_access.go exposes what the execution module (Slice 5) needs from
// a Connection to call a provider on the user's behalf, without ever handing
// a raw token to a DTO or a log line: the vault stays private to this
// module.
package connections

import (
	"context"

	"beecon/internal/organizations"
)

// ExecutionAccess is what a tool execution needs from a Connection: its
// current status (a non-ACTIVE connection is a tool-level failure the caller
// decides how to report, AC4) and — only meaningful when ACTIVE — the
// connection's access token, decrypted for this one call. It is never held
// longer than the single provider call it authorizes, put on a DTO, or
// written to a log line unredacted.
type ExecutionAccess struct {
	Status      Status
	AccessToken string
}

// ResolveForExecution looks up a Connection scoped to org and confirms it
// belongs to userID. A connection that does not exist, belongs to another
// organization, or does not belong to userID is indistinguishable —
// ErrNotFound, no existence leak (AC5, AC6). A non-ACTIVE connection is
// returned with its status and no access token, letting the caller decide
// how to report that as a tool-level failure (AC4) rather than an HTTP
// error.
func (f *Facade) ResolveForExecution(ctx context.Context, org organizations.OrgID, userID organizations.UserID, id ConnectionID) (ExecutionAccess, error) {
	connection, err := f.repo.FindByID(ctx, org, id)
	if err != nil {
		return ExecutionAccess{}, err
	}
	if connection == nil || connection.UserID != userID {
		return ExecutionAccess{}, ErrNotFound()
	}
	if connection.Status != StatusActive {
		return ExecutionAccess{Status: connection.Status}, nil
	}

	accessToken, err := f.vault.Decrypt(connection.EncryptedAccessToken)
	if err != nil {
		return ExecutionAccess{}, err
	}
	return ExecutionAccess{Status: connection.Status, AccessToken: accessToken}, nil
}
