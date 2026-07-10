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
