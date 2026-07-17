package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/catalog"
)

// ActivatedDefinitionRow is the catalog_activated_definitions table schema
// (migration 0023, Phase 5 registry sub-phase PD65): one row per provider
// slug — this installation is pinned to exactly one activated version per
// provider at a time (PD66).
type ActivatedDefinitionRow struct {
	upstreambun.BaseModel `bun:"table:catalog_activated_definitions,alias:cad"`

	ProviderSlug string    `bun:"provider_slug,pk"`
	Version      string    `bun:"version,notnull"`
	ContentHash  string    `bun:"content_hash,notnull"`
	BundleJSON   string    `bun:"bundle_json,notnull"`
	ActivatedAt  time.Time `bun:"activated_at,notnull"`
}

// ActivatedDefinitionRepository is the bun-backed catalog.ActivatedDefinitions.
type ActivatedDefinitionRepository struct {
	db *upstreambun.DB
}

var _ catalog.ActivatedDefinitions = (*ActivatedDefinitionRepository)(nil)

func NewActivatedDefinitionRepository(db *upstreambun.DB) *ActivatedDefinitionRepository {
	return &ActivatedDefinitionRepository{db: db}
}

// Save upserts activated's row (an insert for a provider's first
// activation, an update for every one after) — plain
// find-then-insert-or-update rather than a dialect-specific ON CONFLICT
// clause, matching every other repository in this codebase
// (organizations.Repository.SaveGovernance is the precedent).
func (r *ActivatedDefinitionRepository) Save(ctx context.Context, activated catalog.ActivatedDefinition) error {
	row := activatedDefinitionRowFrom(activated)
	existing := new(ActivatedDefinitionRow)
	err := r.db.NewSelect().
		Model(existing).
		Where("provider_slug = ?", row.ProviderSlug).
		Limit(1).
		Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = r.db.NewInsert().Model(&row).Exec(ctx)
		return err
	}
	_, err = r.db.NewUpdate().
		Model(&row).
		Column("version", "content_hash", "bundle_json", "activated_at").
		Where("provider_slug = ?", row.ProviderSlug).
		Exec(ctx)
	return err
}

func (r *ActivatedDefinitionRepository) FindByProviderSlug(ctx context.Context, providerSlug string) (*catalog.ActivatedDefinition, error) {
	row := new(ActivatedDefinitionRow)
	err := r.db.NewSelect().
		Model(row).
		Where("provider_slug = ?", providerSlug).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	activated := activatedDefinitionFromRow(row)
	return &activated, nil
}

// Delete removes providerSlug's row entirely (Slice 4, PD66): Activate's own
// rollback path uses this to undo a persisted row it just wrote when a
// later step in the same activation fails and this provider had never been
// activated before — a no-op deleting zero rows is not an error.
func (r *ActivatedDefinitionRepository) Delete(ctx context.Context, providerSlug string) error {
	_, err := r.db.NewDelete().
		Model((*ActivatedDefinitionRow)(nil)).
		Where("provider_slug = ?", providerSlug).
		Exec(ctx)
	return err
}

func (r *ActivatedDefinitionRepository) ListAll(ctx context.Context) ([]catalog.ActivatedDefinition, error) {
	var rows []ActivatedDefinitionRow
	err := r.db.NewSelect().
		Model(&rows).
		Order("provider_slug ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	activated := make([]catalog.ActivatedDefinition, 0, len(rows))
	for _, row := range rows {
		activated = append(activated, activatedDefinitionFromRow(&row))
	}
	return activated, nil
}

func activatedDefinitionRowFrom(activated catalog.ActivatedDefinition) ActivatedDefinitionRow {
	return ActivatedDefinitionRow{
		ProviderSlug: activated.ProviderSlug,
		Version:      activated.Version,
		ContentHash:  activated.ContentHash,
		BundleJSON:   activated.BundleJSON,
		ActivatedAt:  activated.ActivatedAt,
	}
}

func activatedDefinitionFromRow(row *ActivatedDefinitionRow) catalog.ActivatedDefinition {
	return catalog.ActivatedDefinition{
		ProviderSlug: row.ProviderSlug,
		Version:      row.Version,
		ContentHash:  row.ContentHash,
		BundleJSON:   row.BundleJSON,
		ActivatedAt:  row.ActivatedAt,
	}
}
