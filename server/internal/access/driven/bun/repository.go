// Package bun is the access module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// ServerApiKeyRow is the server_api_keys table schema.
type ServerApiKeyRow struct {
	upstreambun.BaseModel `bun:"table:server_api_keys,alias:k"`

	ID           string     `bun:"id,pk"`
	OrgID        string     `bun:"org_id,notnull"`
	LookupPrefix string     `bun:"lookup_prefix,notnull"`
	SecretHash   string     `bun:"secret_hash,notnull"`
	CreatedAt    time.Time  `bun:"created_at,notnull"`
	RevokedAt    *time.Time `bun:"revoked_at"`
}

// Repository is the bun-backed access.Repository and access.PrefixLookup.
type Repository struct {
	db *upstreambun.DB
}

var _ access.Repository = (*Repository)(nil)
var _ access.PrefixLookup = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(ctx context.Context, key access.ServerApiKey) error {
	row := rowFromKey(key)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) ListByOrg(ctx context.Context, org organizations.OrgID) ([]access.ServerApiKey, error) {
	var rows []ServerApiKeyRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("org_id = ?", string(org)).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return keysFromRows(rows)
}

func (r *Repository) FindByID(ctx context.Context, org organizations.OrgID, id access.KeyID) (*access.ServerApiKey, error) {
	row := new(ServerApiKeyRow)
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
	key, err := keyFromRow(row)
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (r *Repository) MarkRevoked(ctx context.Context, org organizations.OrgID, id access.KeyID, revokedAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*ServerApiKeyRow)(nil)).
		Set("revoked_at = ?", revokedAt).
		Where("id = ?", string(id)).
		Where("org_id = ?", string(org)).
		Exec(ctx)
	return err
}

func (r *Repository) FindByPrefix(ctx context.Context, prefix string) ([]access.ServerApiKey, error) {
	var rows []ServerApiKeyRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("lookup_prefix = ?", prefix).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return keysFromRows(rows)
}

func rowFromKey(key access.ServerApiKey) ServerApiKeyRow {
	return ServerApiKeyRow{
		ID:           string(key.ID),
		OrgID:        string(key.OrgID),
		LookupPrefix: key.LookupPrefix,
		SecretHash:   hex.EncodeToString(key.SecretHash),
		CreatedAt:    key.CreatedAt,
		RevokedAt:    key.RevokedAt,
	}
}

func keyFromRow(row *ServerApiKeyRow) (access.ServerApiKey, error) {
	hashBytes, err := hex.DecodeString(row.SecretHash)
	if err != nil {
		return access.ServerApiKey{}, err
	}
	return access.ServerApiKey{
		ID:           access.KeyID(row.ID),
		OrgID:        organizations.OrgID(row.OrgID),
		LookupPrefix: row.LookupPrefix,
		SecretHash:   hashBytes,
		CreatedAt:    row.CreatedAt,
		RevokedAt:    row.RevokedAt,
	}, nil
}

func keysFromRows(rows []ServerApiKeyRow) ([]access.ServerApiKey, error) {
	keys := make([]access.ServerApiKey, 0, len(rows))
	for _, row := range rows {
		key, err := keyFromRow(&row)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}
