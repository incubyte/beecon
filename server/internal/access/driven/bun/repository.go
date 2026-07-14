// Package bun is the access module's persistence adapter. It is the only
// place in the module that imports database/sql or uptrace/bun; the row
// structs' bun tags are the schema's source of truth.
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

// ServerApiKeyRow is the server_api_keys table schema. Since migration 0012
// (Slice 8, PD23) it carries only the key's own identity — no secret
// material; that lives in ApiKeySecretRow. Since migration 0017 (Slice 4,
// PD41) it also carries Scope; existing rows migrated to "read-write" by
// the migration's column default.
type ServerApiKeyRow struct {
	upstreambun.BaseModel `bun:"table:server_api_keys,alias:k"`

	ID        string     `bun:"id,pk"`
	OrgID     string     `bun:"org_id,notnull"`
	Scope     string     `bun:"scope,notnull"`
	CreatedAt time.Time  `bun:"created_at,notnull"`
	RevokedAt *time.Time `bun:"revoked_at"`
}

// ApiKeySecretRow is the server_api_key_secrets table schema (migration
// 0012): which key a secret belongs to, its lookup prefix and hash, and when
// it stops verifying (PD23's overlap window).
type ApiKeySecretRow struct {
	upstreambun.BaseModel `bun:"table:server_api_key_secrets,alias:s"`

	ID           string     `bun:"id,pk"`
	KeyID        string     `bun:"key_id,notnull"`
	LookupPrefix string     `bun:"lookup_prefix,notnull"`
	SecretHash   string     `bun:"secret_hash,notnull"`
	CreatedAt    time.Time  `bun:"created_at,notnull"`
	ExpiresAt    *time.Time `bun:"expires_at"`
}

// Repository is the bun-backed access.Repository, access.PrefixLookup, and
// access.ApiKeySecrets.
type Repository struct {
	db *upstreambun.DB
}

var _ access.Repository = (*Repository)(nil)
var _ access.PrefixLookup = (*Repository)(nil)
var _ access.ApiKeySecrets = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) SaveKey(ctx context.Context, key access.ServerApiKey) error {
	row := keyRowFrom(key)
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
	keys := make([]access.ServerApiKey, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, keyFromRow(&row))
	}
	return keys, nil
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
	key := keyFromRow(row)
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

// Save inserts secret. org is not used to restrict the write: by the time
// Issue or Rotate calls Save, they have already resolved (or just minted)
// secret.KeyID within org — ListByKeyID and MarkExpiring below are where org
// scoping is actually enforced against the database, since server_api_keys
// is where org_id lives.
func (r *Repository) Save(ctx context.Context, _ organizations.OrgID, secret access.ApiKeySecret) error {
	row := secretRowFrom(secret)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) ListByKeyID(ctx context.Context, org organizations.OrgID, keyID access.KeyID) ([]access.ApiKeySecret, error) {
	var rows []ApiKeySecretRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("key_id = ?", string(keyID)).
		Where("key_id IN (SELECT id FROM server_api_keys WHERE org_id = ?)", string(org)).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return secretsFromRows(rows)
}

func (r *Repository) MarkExpiring(ctx context.Context, org organizations.OrgID, id access.ApiKeySecretID, expiresAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*ApiKeySecretRow)(nil)).
		Set("expires_at = ?", expiresAt).
		Where("id = ?", string(id)).
		Where("key_id IN (SELECT id FROM server_api_keys WHERE org_id = ?)", string(org)).
		Exec(ctx)
	return err
}

// FindByPrefix loads every secret sharing prefix, then the keys they belong
// to, and joins them in Go: prefix collisions are rare and small (PD3), so
// two round trips read more clearly than a hand-written SQL join across
// both dialects.
func (r *Repository) FindByPrefix(ctx context.Context, prefix string) ([]access.ApiKeySecretCandidate, error) {
	var secretRows []ApiKeySecretRow
	err := r.db.NewSelect().
		Model(&secretRows).
		Where("lookup_prefix = ?", prefix).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	if len(secretRows) == 0 {
		return nil, nil
	}
	keyRowsByID, err := r.keyRowsByID(ctx, keyIDsOf(secretRows))
	if err != nil {
		return nil, err
	}
	return candidatesFrom(secretRows, keyRowsByID)
}

func (r *Repository) keyRowsByID(ctx context.Context, ids []string) (map[string]ServerApiKeyRow, error) {
	var rows []ServerApiKeyRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("id IN (?)", upstreambun.List(ids)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]ServerApiKeyRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return byID, nil
}

func keyRowFrom(key access.ServerApiKey) ServerApiKeyRow {
	return ServerApiKeyRow{
		ID:        string(key.ID),
		OrgID:     string(key.OrgID),
		Scope:     string(key.Scope),
		CreatedAt: key.CreatedAt,
		RevokedAt: key.RevokedAt,
	}
}

func keyFromRow(row *ServerApiKeyRow) access.ServerApiKey {
	return access.ServerApiKey{
		ID:        access.KeyID(row.ID),
		OrgID:     organizations.OrgID(row.OrgID),
		Scope:     access.Scope(row.Scope),
		CreatedAt: row.CreatedAt,
		RevokedAt: row.RevokedAt,
	}
}

func secretRowFrom(secret access.ApiKeySecret) ApiKeySecretRow {
	return ApiKeySecretRow{
		ID:           string(secret.ID),
		KeyID:        string(secret.KeyID),
		LookupPrefix: secret.LookupPrefix,
		SecretHash:   hex.EncodeToString(secret.SecretHash),
		CreatedAt:    secret.CreatedAt,
		ExpiresAt:    secret.ExpiresAt,
	}
}

func secretFromRow(row *ApiKeySecretRow) (access.ApiKeySecret, error) {
	hashBytes, err := hex.DecodeString(row.SecretHash)
	if err != nil {
		return access.ApiKeySecret{}, err
	}
	return access.ApiKeySecret{
		ID:           access.ApiKeySecretID(row.ID),
		KeyID:        access.KeyID(row.KeyID),
		LookupPrefix: row.LookupPrefix,
		SecretHash:   hashBytes,
		CreatedAt:    row.CreatedAt,
		ExpiresAt:    row.ExpiresAt,
	}, nil
}

func secretsFromRows(rows []ApiKeySecretRow) ([]access.ApiKeySecret, error) {
	secrets := make([]access.ApiKeySecret, 0, len(rows))
	for i := range rows {
		secret, err := secretFromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

func keyIDsOf(rows []ApiKeySecretRow) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.KeyID]; ok {
			continue
		}
		seen[row.KeyID] = struct{}{}
		ids = append(ids, row.KeyID)
	}
	return ids
}

func candidatesFrom(secretRows []ApiKeySecretRow, keyRowsByID map[string]ServerApiKeyRow) ([]access.ApiKeySecretCandidate, error) {
	candidates := make([]access.ApiKeySecretCandidate, 0, len(secretRows))
	for i := range secretRows {
		keyRow, ok := keyRowsByID[secretRows[i].KeyID]
		if !ok {
			continue
		}
		secret, err := secretFromRow(&secretRows[i])
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, access.ApiKeySecretCandidate{
			KeyID:     access.KeyID(keyRow.ID),
			OrgID:     organizations.OrgID(keyRow.OrgID),
			Scope:     access.Scope(keyRow.Scope),
			RevokedAt: keyRow.RevokedAt,
			Secret:    secret,
		})
	}
	return candidates, nil
}
