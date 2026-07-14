// Package bun is the organizations module's persistence adapter. It is the
// only place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/organizations"
)

// OrganizationRow is the organizations table schema. AllowedRedirectURIs is
// stored as a JSON-encoded array in a single text column so the same schema
// works identically across both the Postgres and SQLite dialects.
type OrganizationRow struct {
	upstreambun.BaseModel `bun:"table:organizations,alias:o"`

	ID                  string    `bun:"id,pk"`
	Name                string    `bun:"name,notnull"`
	AllowedRedirectURIs string    `bun:"allowed_redirect_uris,notnull"`
	CreatedAt           time.Time `bun:"created_at,notnull"`
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
	row, err := rowFromOrganization(org)
	if err != nil {
		return err
	}
	_, err = r.db.NewInsert().Model(&row).Exec(ctx)
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
	org, err := organizationFromRow(row)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// Update persists a previously created Organization's mutable fields (today,
// only the redirect-uri allow-list — PD4).
func (r *Repository) Update(ctx context.Context, org organizations.Organization) error {
	row, err := rowFromOrganization(org)
	if err != nil {
		return err
	}
	_, err = r.db.NewUpdate().
		Model(&row).
		Column("allowed_redirect_uris").
		Where("id = ?", row.ID).
		Exec(ctx)
	return err
}

// ListAll returns every organization in the installation, newest first
// (created_at DESC, id DESC as a deterministic tiebreaker), matching cursor
// and limited to limit rows (Slice 1, PD40) — installation-level:
// Organization is itself the isolation unit, so there is no org id to scope
// this query by.
func (r *Repository) ListAll(ctx context.Context, cursor *organizations.ListAllCursor, limit int) ([]organizations.Organization, error) {
	var rows []OrganizationRow
	query := r.db.NewSelect().Model(&rows)

	if cursor != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			cursor.CreatedAt, cursor.CreatedAt, string(cursor.ID))
	}

	err := query.
		Order("created_at DESC", "id DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]organizations.Organization, 0, len(rows))
	for _, row := range rows {
		org, err := organizationFromRow(&row)
		if err != nil {
			return nil, err
		}
		results = append(results, org)
	}
	return results, nil
}

func rowFromOrganization(org organizations.Organization) (OrganizationRow, error) {
	allowedRedirectURIs, err := json.Marshal(org.AllowedRedirectURIs)
	if err != nil {
		return OrganizationRow{}, err
	}
	return OrganizationRow{
		ID:                  string(org.ID),
		Name:                org.Name,
		AllowedRedirectURIs: string(allowedRedirectURIs),
		CreatedAt:           org.CreatedAt,
	}, nil
}

func organizationFromRow(row *OrganizationRow) (organizations.Organization, error) {
	var allowedRedirectURIs []string
	if row.AllowedRedirectURIs != "" {
		if err := json.Unmarshal([]byte(row.AllowedRedirectURIs), &allowedRedirectURIs); err != nil {
			return organizations.Organization{}, err
		}
	}
	return organizations.Organization{
		ID:                  organizations.OrgID(row.ID),
		Name:                row.Name,
		AllowedRedirectURIs: allowedRedirectURIs,
		CreatedAt:           row.CreatedAt,
	}, nil
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

// ListByOrg returns org's end-users, newest first (created_at DESC, id DESC
// as a deterministic tiebreaker), matching cursor and limited to limit rows
// (Slice 4, PD40) — mirrors ListAll's own ordering and cursor semantics,
// scoped to one organization.
func (r *Repository) ListByOrg(ctx context.Context, org organizations.OrgID, cursor *organizations.UserListCursor, limit int) ([]organizations.User, error) {
	var rows []UserRow
	query := r.db.NewSelect().Model(&rows).Where("org_id = ?", string(org))

	if cursor != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			cursor.CreatedAt, cursor.CreatedAt, string(cursor.ID))
	}

	err := query.
		Order("created_at DESC", "id DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]organizations.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, userFromRow(&row))
	}
	return users, nil
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

// OrgGovernanceRow is the org_governance table schema (migration 0018,
// Slice 5, FD8: one settings table for governance, later retention columns).
// AllowList, Hidden, and Featured are JSON-encoded arrays in text columns,
// matching the AllowedRedirectURIs/OrganizationRow convention. AllowList is
// nullable: NULL means "inherit the full installation catalog" (PD42).
// LogRetentionDays/EventRetentionDays (migration 0019, Slice 7, PD44) are
// plain nullable integer columns — no JSON encoding needed for a single
// optional number — NULL meaning "inherit the installation's own
// BEECON_RETENTION_DAYS default" and 0 meaning unlimited/disabled.
type OrgGovernanceRow struct {
	upstreambun.BaseModel `bun:"table:org_governance,alias:og"`

	OrganizationID     string  `bun:"organization_id,pk"`
	AllowList          *string `bun:"allow_list"`
	Hidden             string  `bun:"hidden,notnull"`
	Featured           string  `bun:"featured,notnull"`
	FeaturedCap        int     `bun:"featured_cap,notnull"`
	LogRetentionDays   *int    `bun:"log_retention_days"`
	EventRetentionDays *int    `bun:"event_retention_days"`
}

var _ organizations.GovernanceRepository = (*Repository)(nil)

// FindByOrg returns org's governance row, or (nil, nil) when org has never
// been configured (Slice 5) — GetGovernance synthesizes the
// continuity-preserving default in that case.
func (r *Repository) FindByOrg(ctx context.Context, org organizations.OrgID) (*organizations.Governance, error) {
	row := new(OrgGovernanceRow)
	err := r.db.NewSelect().
		Model(row).
		Where("organization_id = ?", string(org)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	governance, err := governanceFromRow(row)
	if err != nil {
		return nil, err
	}
	return &governance, nil
}

// SaveGovernance upserts org's governance row (Slice 5): an insert for an
// org's first SetGovernance call, an update for every one after — plain
// find-then-insert-or-update rather than a dialect-specific ON CONFLICT
// clause, matching every other repository in this codebase.
func (r *Repository) SaveGovernance(ctx context.Context, governance organizations.Governance) error {
	row, err := rowFromGovernance(governance)
	if err != nil {
		return err
	}
	existing := new(OrgGovernanceRow)
	err = r.db.NewSelect().
		Model(existing).
		Where("organization_id = ?", row.OrganizationID).
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
		Column("allow_list", "hidden", "featured", "featured_cap", "log_retention_days", "event_retention_days").
		Where("organization_id = ?", row.OrganizationID).
		Exec(ctx)
	return err
}

func rowFromGovernance(governance organizations.Governance) (OrgGovernanceRow, error) {
	var allowList *string
	if governance.AllowList != nil {
		encoded, err := json.Marshal(*governance.AllowList)
		if err != nil {
			return OrgGovernanceRow{}, err
		}
		value := string(encoded)
		allowList = &value
	}
	hidden, err := json.Marshal(governance.Hidden)
	if err != nil {
		return OrgGovernanceRow{}, err
	}
	featured, err := json.Marshal(governance.Featured)
	if err != nil {
		return OrgGovernanceRow{}, err
	}
	return OrgGovernanceRow{
		OrganizationID:     string(governance.OrgID),
		AllowList:          allowList,
		Hidden:             string(hidden),
		Featured:           string(featured),
		FeaturedCap:        governance.FeaturedCap,
		LogRetentionDays:   copyIntPtrForRow(governance.LogRetentionDays),
		EventRetentionDays: copyIntPtrForRow(governance.EventRetentionDays),
	}, nil
}

// copyIntPtrForRow defensively copies a *int before it crosses into the row
// struct — mirrors organizations.Governance's own copyIntPtr, kept local to
// this adapter since that helper is unexported in the domain package.
func copyIntPtrForRow(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func governanceFromRow(row *OrgGovernanceRow) (organizations.Governance, error) {
	var allowList *[]string
	if row.AllowList != nil {
		var parsed []string
		if err := json.Unmarshal([]byte(*row.AllowList), &parsed); err != nil {
			return organizations.Governance{}, err
		}
		allowList = &parsed
	}
	var hidden []string
	if row.Hidden != "" {
		if err := json.Unmarshal([]byte(row.Hidden), &hidden); err != nil {
			return organizations.Governance{}, err
		}
	}
	var featured []string
	if row.Featured != "" {
		if err := json.Unmarshal([]byte(row.Featured), &featured); err != nil {
			return organizations.Governance{}, err
		}
	}
	return organizations.Governance{
		OrgID:              organizations.OrgID(row.OrganizationID),
		AllowList:          allowList,
		Hidden:             hidden,
		Featured:           featured,
		FeaturedCap:        row.FeaturedCap,
		LogRetentionDays:   copyIntPtrForRow(row.LogRetentionDays),
		EventRetentionDays: copyIntPtrForRow(row.EventRetentionDays),
	}, nil
}
