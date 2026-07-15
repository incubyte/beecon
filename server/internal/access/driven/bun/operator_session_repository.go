package bun

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
)

// OperatorSessionRow is the operator_sessions table schema (migration
// 0021). TokenHash is stored hex-encoded, mirroring ApiKeySecretRow's own
// SecretHash convention.
type OperatorSessionRow struct {
	upstreambun.BaseModel `bun:"table:operator_sessions,alias:opsess"`

	ID         string     `bun:"id,pk"`
	OperatorID string     `bun:"operator_id,notnull"`
	TokenHash  string     `bun:"token_hash,notnull"`
	CSRFToken  string     `bun:"csrf_token,notnull"`
	CreatedAt  time.Time  `bun:"created_at,notnull"`
	ExpiresAt  time.Time  `bun:"expires_at,notnull"`
	RevokedAt  *time.Time `bun:"revoked_at"`
}

// OperatorSessionRepository is the bun-backed access.OperatorSessions
// (PD51): installation-level, no organization_id anywhere on this table.
type OperatorSessionRepository struct {
	db *upstreambun.DB
}

var _ access.OperatorSessions = (*OperatorSessionRepository)(nil)

func NewOperatorSessionRepository(db *upstreambun.DB) *OperatorSessionRepository {
	return &OperatorSessionRepository{db: db}
}

func (r *OperatorSessionRepository) Save(ctx context.Context, session access.OperatorSession) error {
	row := operatorSessionRowFrom(session)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *OperatorSessionRepository) FindByTokenHash(ctx context.Context, tokenHash []byte) (*access.OperatorSession, error) {
	row := new(OperatorSessionRow)
	err := r.db.NewSelect().
		Model(row).
		Where("token_hash = ?", hex.EncodeToString(tokenHash)).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	session, err := operatorSessionFromRow(row)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// Revoke marks session id's revoked_at (Slice 2's Logout). Idempotent: the
// WHERE clause's own "revoked_at IS NULL" makes revoking an already-revoked
// (or unknown) id a zero-row, error-free no-op rather than clobbering an
// earlier revocation instant.
func (r *OperatorSessionRepository) Revoke(ctx context.Context, id access.OperatorSessionID, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorSessionRow)(nil)).
		Set("revoked_at = ?", at).
		Where("id = ?", string(id)).
		Where("revoked_at IS NULL").
		Exec(ctx)
	return err
}

// RevokeAllForOperator marks every one of operatorID's still-active sessions
// revoked_at (Slice 4's deactivate/break-glass reset-password paths) in one
// statement.
func (r *OperatorSessionRepository) RevokeAllForOperator(ctx context.Context, operatorID access.OperatorID, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorSessionRow)(nil)).
		Set("revoked_at = ?", at).
		Where("operator_id = ?", string(operatorID)).
		Where("revoked_at IS NULL").
		Exec(ctx)
	return err
}

// RevokeAllForOperatorExcept marks every one of operatorID's still-active
// sessions revoked_at EXCEPT exceptSessionID (Slice 4's ChangeMyPassword,
// carry-forward Slice 2 AC4): the acting session's own row is excluded by
// the "id <> ?" clause, so it survives the same statement that ends every
// other one of the operator's sessions.
func (r *OperatorSessionRepository) RevokeAllForOperatorExcept(ctx context.Context, operatorID access.OperatorID, exceptSessionID access.OperatorSessionID, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorSessionRow)(nil)).
		Set("revoked_at = ?", at).
		Where("operator_id = ?", string(operatorID)).
		Where("id <> ?", string(exceptSessionID)).
		Where("revoked_at IS NULL").
		Exec(ctx)
	return err
}

func operatorSessionRowFrom(session access.OperatorSession) OperatorSessionRow {
	return OperatorSessionRow{
		ID:         string(session.ID),
		OperatorID: string(session.OperatorID),
		TokenHash:  hex.EncodeToString(session.TokenHash),
		CSRFToken:  session.CSRFToken,
		CreatedAt:  session.CreatedAt,
		ExpiresAt:  session.ExpiresAt,
		RevokedAt:  session.RevokedAt,
	}
}

func operatorSessionFromRow(row *OperatorSessionRow) (access.OperatorSession, error) {
	tokenHash, err := hex.DecodeString(row.TokenHash)
	if err != nil {
		return access.OperatorSession{}, err
	}
	return access.OperatorSession{
		ID:         access.OperatorSessionID(row.ID),
		OperatorID: access.OperatorID(row.OperatorID),
		TokenHash:  tokenHash,
		CSRFToken:  row.CSRFToken,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		RevokedAt:  row.RevokedAt,
	}, nil
}
