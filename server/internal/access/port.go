package access

import (
	"context"
	"time"

	"beecon/internal/organizations"
)

// Repository is the access module's org-scoped driven port. Every method
// takes the owning OrgID as its second parameter, so a query without org
// scope cannot be expressed.
type Repository interface {
	Save(ctx context.Context, key ServerApiKey) error
	ListByOrg(ctx context.Context, org organizations.OrgID) ([]ServerApiKey, error)
	FindByID(ctx context.Context, org organizations.OrgID, id KeyID) (*ServerApiKey, error)
	MarkRevoked(ctx context.Context, org organizations.OrgID, id KeyID, revokedAt time.Time) error
}

// PrefixLookup is deliberately installation-level, not org-scoped: Verify
// authenticates a presented secret before the caller's organization is
// known — the lookup prefix is how Verify discovers which key (and which
// organization) was presented in the first place. Because the lookup prefix
// carries only LookupPrefixLength characters, more than one key may share a
// prefix, so this returns every candidate; Verify picks the one whose
// secret hash actually matches.
type PrefixLookup interface {
	FindByPrefix(ctx context.Context, prefix string) ([]ServerApiKey, error)
}
