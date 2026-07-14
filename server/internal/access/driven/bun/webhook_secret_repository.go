package bun

import (
	"context"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
	"beecon/internal/organizations"
)

// WebhookSigningSecretRow is the webhook_signing_secrets table schema.
// EndpointID (migration 0020, Slice 8) is a nullable column at the SQL
// level — the migration backfills every pre-existing Phase 3 row's
// endpoint_id onto that org's single (now "first") endpoint — but the
// access facade itself always sets it on every row it writes from Slice 8
// onward, so a *string here only ever guards against an unbackfilled row,
// never a normal write path.
type WebhookSigningSecretRow struct {
	upstreambun.BaseModel `bun:"table:webhook_signing_secrets,alias:whs"`

	ID              string     `bun:"id,pk"`
	OrgID           string     `bun:"organization_id,notnull"`
	EndpointID      *string    `bun:"endpoint_id"`
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

// ListByEndpoint returns org's secrets scoped to one specific endpoint
// (Slice 8: many endpoints per org, each with its own secret lineage) —
// mirrors the old ListByOrg's ordering (oldest first).
func (r *WebhookSecretRepository) ListByEndpoint(ctx context.Context, org organizations.OrgID, endpoint access.EndpointID) ([]access.WebhookSigningSecret, error) {
	var rows []WebhookSigningSecretRow
	err := r.db.NewSelect().
		Model(&rows).
		Where("organization_id = ?", string(org)).
		Where("endpoint_id = ?", string(endpoint)).
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
	endpointID := string(secret.EndpointID)
	return WebhookSigningSecretRow{
		ID:              string(secret.ID),
		OrgID:           string(secret.OrgID),
		EndpointID:      &endpointID,
		DisplayPrefix:   secret.DisplayPrefix,
		EncryptedSecret: secret.EncryptedSecret,
		CreatedAt:       secret.CreatedAt,
		ExpiresAt:       secret.ExpiresAt,
	}
}

func webhookSecretFromRow(row *WebhookSigningSecretRow) access.WebhookSigningSecret {
	var endpointID access.EndpointID
	if row.EndpointID != nil {
		endpointID = access.EndpointID(*row.EndpointID)
	}
	return access.WebhookSigningSecret{
		ID:              access.WebhookSecretID(row.ID),
		OrgID:           organizations.OrgID(row.OrgID),
		EndpointID:      endpointID,
		DisplayPrefix:   row.DisplayPrefix,
		EncryptedSecret: row.EncryptedSecret,
		CreatedAt:       row.CreatedAt,
		ExpiresAt:       row.ExpiresAt,
	}
}
