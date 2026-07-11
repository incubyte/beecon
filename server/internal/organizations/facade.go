package organizations

import (
	"context"
	"time"
)

// Facade is the organizations module's only public surface.
type Facade struct {
	repo      Repository
	users     UserRepository
	newOrgID  func() string
	newUserID func() string
	now       func() time.Time
}

// NewFacade wires the facade with an injected id minter per entity and a
// clock so tests can supply deterministic ids and a fixed time.
func NewFacade(repo Repository, users UserRepository, newOrgID, newUserID func() string, now func() time.Time) *Facade {
	return &Facade{repo: repo, users: users, newOrgID: newOrgID, newUserID: newUserID, now: now}
}

// Create validates name and persists a new Organization.
func (f *Facade) Create(ctx context.Context, name string) (Organization, error) {
	org, err := NewOrganization(OrgID(f.newOrgID()), name, f.now())
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

// CreateUser validates and persists a new User scoped to org (PD2: the
// consumer's own server provisions its users with its org API key).
func (f *Facade) CreateUser(ctx context.Context, org OrgID, name, externalID string) (User, error) {
	user, err := NewUser(UserID(f.newUserID()), org, name, externalID, f.now())
	if err != nil {
		return User{}, err
	}
	if err := f.users.SaveUser(ctx, user); err != nil {
		return User{}, err
	}
	return user, nil
}

// SetAllowedRedirectURIs replaces org's redirect-uri allow-list (PD4),
// settable only by the installation admin (PATCH
// /api/v1/organizations/{orgId}).
func (f *Facade) SetAllowedRedirectURIs(ctx context.Context, id OrgID, uris []string) (Organization, error) {
	org, err := f.repo.FindByID(ctx, id)
	if err != nil {
		return Organization{}, err
	}
	if org == nil {
		return Organization{}, ErrNotFound()
	}
	updated := org.WithAllowedRedirectURIs(uris)
	if err := f.repo.Update(ctx, updated); err != nil {
		return Organization{}, err
	}
	return updated, nil
}

// GetUser fetches a User scoped to org, translating a repository miss (or a
// cross-org match) into ErrUserNotFound.
func (f *Facade) GetUser(ctx context.Context, org OrgID, id UserID) (User, error) {
	user, err := f.users.FindUserByID(ctx, org, id)
	if err != nil {
		return User{}, err
	}
	if user == nil {
		return User{}, ErrUserNotFound()
	}
	return *user, nil
}
