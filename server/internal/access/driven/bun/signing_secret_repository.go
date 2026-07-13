package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// SigningSecretRow is the signing_secrets table schema.
type SigningSecretRow struct {
	upstreambun.BaseModel `bun:"table:signing_secrets,alias:s"`

	ID              string    `bun:"id,pk"`
	OrgID           string    `bun:"org_id,notnull"`
	DisplayPrefix   string    `bun:"display_prefix,notnull"`
	EncryptedSecret string    `bun:"encrypted_secret,notnull"`
	CreatedAt       time.Time `bun:"created_at,notnull"`
}

// SigningSecretRepository is the bun-backed access.SigningSecrets and
// access.SigningSecretLookup. It is a separate type from Repository (not
// additional methods on it): access.Repository and access.SigningSecrets
// both declare a Save(ctx, entity) method — Go has no method overloading, so
// the two storage shapes need their own types.
type SigningSecretRepository struct {
	db *upstreambun.DB
}

var _ access.SigningSecrets = (*SigningSecretRepository)(nil)
var _ access.SigningSecretLookup = (*SigningSecretRepository)(nil)

func NewSigningSecretRepository(db *upstreambun.DB) *SigningSecretRepository {
	return &SigningSecretRepository{db: db}
}

func (r *SigningSecretRepository) Save(ctx context.Context, secret access.SigningSecret) error {
	row := signingSecretRowFrom(secret)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *SigningSecretRepository) ListByOrg(ctx context.Context, org organizations.OrgID) ([]access.SigningSecret, error) {
	var rows []SigningSecretRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("org_id = ?", string(org)).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	secrets := make([]access.SigningSecret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, signingSecretFromRow(&row))
	}
	return secrets, nil
}

func (r *SigningSecretRepository) FindByKid(ctx context.Context, id access.SigningSecretID) (*access.SigningSecret, error) {
	row := new(SigningSecretRow)
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
	secret := signingSecretFromRow(row)
	return &secret, nil
}

func signingSecretRowFrom(secret access.SigningSecret) SigningSecretRow {
	return SigningSecretRow{
		ID:              string(secret.ID),
		OrgID:           string(secret.OrgID),
		DisplayPrefix:   secret.DisplayPrefix,
		EncryptedSecret: secret.EncryptedSecret,
		CreatedAt:       secret.CreatedAt,
	}
}

func signingSecretFromRow(row *SigningSecretRow) access.SigningSecret {
	return access.SigningSecret{
		ID:              access.SigningSecretID(row.ID),
		OrgID:           organizations.OrgID(row.OrgID),
		DisplayPrefix:   row.DisplayPrefix,
		EncryptedSecret: row.EncryptedSecret,
		CreatedAt:       row.CreatedAt,
	}
}
