// Package bun is the organizations module's persistence adapter. It is the
// only place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/organizations"
)

// OrganizationRow is the organizations table schema.
type OrganizationRow struct {
	upstreambun.BaseModel `bun:"table:organizations,alias:o"`

	ID        string    `bun:"id,pk"`
	Name      string    `bun:"name,notnull"`
	CreatedAt time.Time `bun:"created_at,notnull"`
}

// Repository is the bun-backed organizations.Repository.
type Repository struct {
	db *upstreambun.DB
}

var _ organizations.Repository = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(ctx context.Context, org organizations.Organization) error {
	row := rowFromOrganization(org)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id organizations.OrgID) (*organizations.Organization, error) {
	row := new(OrganizationRow)
	err := r.db.NewSelect().
		Model(row).
		Where("id = ?", string(id)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	org := organizationFromRow(row)
	return &org, nil
}

func rowFromOrganization(org organizations.Organization) OrganizationRow {
	return OrganizationRow{
		ID:        string(org.ID),
		Name:      org.Name,
		CreatedAt: org.CreatedAt,
	}
}

func organizationFromRow(row *OrganizationRow) organizations.Organization {
	return organizations.Organization{
		ID:        organizations.OrgID(row.ID),
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
	}
}

// UserRow is the users table schema.
type UserRow struct {
	upstreambun.BaseModel `bun:"table:users,alias:u"`

	ID         string    `bun:"id,pk"`
	OrgID      string    `bun:"org_id,notnull"`
	Name       string    `bun:"name,notnull"`
	ExternalID string    `bun:"external_id,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
}

var _ organizations.UserRepository = (*Repository)(nil)

func (r *Repository) SaveUser(ctx context.Context, user organizations.User) error {
	row := userRowFromUser(user)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) FindUserByID(ctx context.Context, org organizations.OrgID, id organizations.UserID) (*organizations.User, error) {
	row := new(UserRow)
	err := r.db.NewSelect().
		Model(row).
		Where("id = ?", string(id)).
		Where("org_id = ?", string(org)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	user := userFromRow(row)
	return &user, nil
}

func userRowFromUser(user organizations.User) UserRow {
	return UserRow{
		ID:         string(user.ID),
		OrgID:      string(user.OrgID),
		Name:       user.Name,
		ExternalID: user.ExternalID,
		CreatedAt:  user.CreatedAt,
	}
}

func userFromRow(row *UserRow) organizations.User {
	return organizations.User{
		ID:         organizations.UserID(row.ID),
		OrgID:      organizations.OrgID(row.OrgID),
		Name:       row.Name,
		ExternalID: row.ExternalID,
		CreatedAt:  row.CreatedAt,
	}
}
