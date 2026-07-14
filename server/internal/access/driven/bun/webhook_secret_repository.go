package bun

import (
	"context"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// WebhookSigningSecretRow is the webhook_signing_secrets table schema.
type WebhookSigningSecretRow struct {
	upstreambun.BaseModel `bun:"table:webhook_signing_secrets,alias:whs"`

	ID              string     `bun:"id,pk"`
	OrgID           string     `bun:"organization_id,notnull"`
	DisplayPrefix   string     `bun:"display_prefix,notnull"`
	EncryptedSecret string     `bun:"encrypted_secret,notnull"`
	CreatedAt       time.Time  `bun:"created_at,notnull"`
	ExpiresAt       *time.Time `bun:"expires_at"`
}

// WebhookSecretRepository is the bun-backed access.WebhookSecrets. It is a
// separate type from Repository/SigningSecretRepository (not additional
// methods on either) for the same reason those two are already split:
// access.ApiKeySecrets, access.SigningSecrets, and access.WebhookSecrets
// each declare their own Save(ctx, entity) method, and Go has no method
// overloading.
type WebhookSecretRepository struct {
	db *upstreambun.DB
}

var _ access.WebhookSecrets = (*WebhookSecretRepository)(nil)

func NewWebhookSecretRepository(db *upstreambun.DB) *WebhookSecretRepository {
	return &WebhookSecretRepository{db: db}
}

func (r *WebhookSecretRepository) Save(ctx context.Context, secret access.WebhookSigningSecret) error {
	row := webhookSecretRowFrom(secret)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *WebhookSecretRepository) ListByOrg(ctx context.Context, org organizations.OrgID) ([]access.WebhookSigningSecret, error) {
	var rows []WebhookSigningSecretRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("organization_id = ?", string(org)).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	secrets := make([]access.WebhookSigningSecret, 0, len(rows))
	for _, row := range rows {
		secrets = append(secrets, webhookSecretFromRow(&row))
	}
	return secrets, nil
}

func (r *WebhookSecretRepository) MarkExpiring(ctx context.Context, org organizations.OrgID, id access.WebhookSecretID, expiresAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*WebhookSigningSecretRow)(nil)).
		Set("expires_at = ?", expiresAt).
		Where("id = ?", string(id)).
		Where("organization_id = ?", string(org)).
		Exec(ctx)
	return err
}

func webhookSecretRowFrom(secret access.WebhookSigningSecret) WebhookSigningSecretRow {
	return WebhookSigningSecretRow{
		ID:              string(secret.ID),
		OrgID:           string(secret.OrgID),
		DisplayPrefix:   secret.DisplayPrefix,
		EncryptedSecret: secret.EncryptedSecret,
		CreatedAt:       secret.CreatedAt,
		ExpiresAt:       secret.ExpiresAt,
	}
}

func webhookSecretFromRow(row *WebhookSigningSecretRow) access.WebhookSigningSecret {
	return access.WebhookSigningSecret{
		ID:              access.WebhookSecretID(row.ID),
		OrgID:           organizations.OrgID(row.OrgID),
		DisplayPrefix:   row.DisplayPrefix,
		EncryptedSecret: row.EncryptedSecret,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
}
