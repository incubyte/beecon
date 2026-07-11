// Package bun is the catalog module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/catalog"
)

// IntegrationRow is the integrations table schema.
type IntegrationRow struct {
	upstreambun.BaseModel `bun:"table:integrations,alias:i"`

	ID           string    `bun:"id,pk"`
	ProviderSlug string    `bun:"provider_slug,notnull"`
	ClientID     string    `bun:"client_id,notnull"`
	ClientSecret string    `bun:"client_secret,notnull"`
	CreatedAt    time.Time `bun:"created_at,notnull"`
}

// Repository is the bun-backed catalog.Repository.
type Repository struct {
	db *upstreambun.DB
}

var _ catalog.Repository = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(ctx context.Context, integration catalog.Integration) error {
	row := rowFromIntegration(integration)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id catalog.IntegrationID) (*catalog.Integration, error) {
	row := new(IntegrationRow)
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
	integration := integrationFromRow(row)
	return &integration, nil
}

func (r *Repository) ListAll(ctx context.Context) ([]catalog.Integration, error) {
	var rows []IntegrationRow
	err := r.db.NewSelect().
		Model(&rows).
		Order("created_at ASC", "id ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	integrations := make([]catalog.Integration, 0, len(rows))
	for _, row := range rows {
		integrations = append(integrations, integrationFromRow(&row))
	}
	return integrations, nil
}

func rowFromIntegration(integration catalog.Integration) IntegrationRow {
	return IntegrationRow{
		ID:           string(integration.ID),
		ProviderSlug: integration.ProviderSlug,
		ClientID:     integration.ClientID,
		ClientSecret: integration.ClientSecret,
		CreatedAt:    integration.CreatedAt,
	}
}

func integrationFromRow(row *IntegrationRow) catalog.Integration {
	return catalog.Integration{
		ID:           catalog.IntegrationID(row.ID),
		ProviderSlug: row.ProviderSlug,
		ClientID:     row.ClientID,
		ClientSecret: row.ClientSecret,
		CreatedAt:    row.CreatedAt,
	}
}
