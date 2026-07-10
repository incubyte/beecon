package organizations

import (
	"context"
	"time"
)

// Facade is the organizations module's only public surface.
type Facade struct {
	repo  Repository
	newID func() string
	now   func() time.Time
}

// NewFacade wires the facade with an injected id minter and clock so tests
// can supply deterministic ids and a fixed time.
func NewFacade(repo Repository, newID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, newID: newID, now: now}
}

// Create validates name and persists a new Organization.
func (f *Facade) Create(ctx context.Context, name string) (Organization, error) {
	org, err := NewOrganization(OrgID(f.newID()), name, f.now())
	if err != nil {
		return Organization{}, err
	}
	if err := f.repo.Save(ctx, org); err != nil {
		return Organization{}, err
	}
	return org, nil
}

// Get fetches an Organization by id, translating a repository miss into
// ErrNotFound.
func (f *Facade) Get(ctx context.Context, id OrgID) (Organization, error) {
	org, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Organization{}, err
	}
	if org == nil {
		return Organization{}, ErrNotFound()
	}
	return *org, nil
}
