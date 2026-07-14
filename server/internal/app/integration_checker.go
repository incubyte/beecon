package app

import (
	"context"
	"errors"

	"beecon/internal/catalog"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// catalogIntegrationChecker satisfies
// organizations.IntegrationExistenceChecker over *catalog.Facade (Slice 9,
// PD46): organizations cannot import catalog (BOUNDARIES — the dependency
// points the other way, catalog already depends on organizations) — config
// import's dry-run reaches the installed-integration existence check only
// through this composition-root adapter.
type catalogIntegrationChecker struct {
	catalog *catalog.Facade
}

var _ organizations.IntegrationExistenceChecker = catalogIntegrationChecker{}

// IntegrationExists satisfies organizations.IntegrationExistenceChecker:
// true when id names an Integration installed anywhere in this
// installation. Deliberately governance-unfiltered (GetIntegration, not
// GetVisibleIntegration): an import's dry-run asks "does this id exist at
// all", not "can this one org see it" — those are different questions, and
// governance visibility is exactly the setting the import is about to
// write.
func (a catalogIntegrationChecker) IntegrationExists(ctx context.Context, id string) (bool, error) {
	_, err := a.catalog.GetIntegration(ctx, catalog.IntegrationID(id))
	if err == nil {
		return true, nil
	}
	var domainErr *httpx.DomainError
	if errors.As(err, &domainErr) && domainErr.Code == catalog.CodeNotFound {
		return false, nil
	}
	return false, err
}
