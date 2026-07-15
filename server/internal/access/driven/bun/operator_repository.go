package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/access"
)

// OperatorRow is the operator_accounts table schema (migration 0021;
// FailedAttempts/LockedUntil added by migration 0022, Slice 5).
type OperatorRow struct {
	upstreambun.BaseModel `bun:"table:operator_accounts,alias:op"`

	ID             string     `bun:"id,pk"`
	Email          string     `bun:"email,notnull"`
	PasswordHash   string     `bun:"password_hash,notnull"`
	Status         string     `bun:"status,notnull"`
	FailedAttempts int        `bun:"failed_attempts,notnull"`
	LockedUntil    *time.Time `bun:"locked_until"`
	CreatedAt      time.Time  `bun:"created_at,notnull"`
}

// OperatorRepository is the bun-backed access.Operators (PD58):
// installation-level, no organization_id anywhere on this table.
type OperatorRepository struct {
	db *upstreambun.DB
}

var _ access.Operators = (*OperatorRepository)(nil)

func NewOperatorRepository(db *upstreambun.DB) *OperatorRepository {
	return &OperatorRepository{db: db}
}

func (r *OperatorRepository) Save(ctx context.Context, operator access.Operator) error {
	row := operatorRowFrom(operator)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *OperatorRepository) FindByEmail(ctx context.Context, email string) (*access.Operator, error) {
	row := new(OperatorRow)
	err := r.db.NewSelect().Model(row).Where("email = ?", email).Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	operator := operatorFromRow(row)
	return &operator, nil
}

func (r *OperatorRepository) FindByID(ctx context.Context, id access.OperatorID) (*access.Operator, error) {
	row := new(OperatorRow)
	err := r.db.NewSelect().Model(row).Where("id = ?", string(id)).Limit(1).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	operator := operatorFromRow(row)
	return &operator, nil
}

func (r *OperatorRepository) Exists(ctx context.Context) (bool, error) {
	count, err := r.db.NewSelect().Model((*OperatorRow)(nil)).Count(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListAll returns every operator account (Slice 4's ListOperators).
func (r *OperatorRepository) ListAll(ctx context.Context) ([]access.Operator, error) {
	var rows []OperatorRow
	if err := r.db.NewSelect().Model(&rows).Scan(ctx); err != nil {
		return nil, err
	}
	operators := make([]access.Operator, len(rows))
	for i, row := range rows {
		operators[i] = operatorFromRow(&row)
	}
	return operators, nil
}

// UpdatePasswordHash overwrites id's password_hash (Slice 4's
// ChangeMyPassword and the break-glass ResetPassword).
func (r *OperatorRepository) UpdatePasswordHash(ctx context.Context, id access.OperatorID, passwordHash string) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorRow)(nil)).
		Set("password_hash = ?", passwordHash).
		Where("id = ?", string(id)).
		Exec(ctx)
	return err
}

// SetStatus overwrites id's status (Slice 4's Deactivate and ResetPassword's
// own reactivation).
func (r *OperatorRepository) SetStatus(ctx context.Context, id access.OperatorID, status access.OperatorStatus) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorRow)(nil)).
		Set("status = ?", string(status)).
		Where("id = ?", string(id)).
		Exec(ctx)
	return err
}

// CountActive counts operator_accounts rows currently status = 'ACTIVE'
// (Deactivate's last-active-operator guard, Slice 4 AC6).
func (r *OperatorRepository) CountActive(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*OperatorRow)(nil)).
		Where("status = ?", string(access.OperatorStatusActive)).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// RecordFailedAttempt increments id's failed_attempts by one (Slice 5's
// brute-force lockout, FD-G) and, only when lockedUntil is non-nil, also
// sets locked_until — the caller (OperatorFacade.Login) has already decided
// whether this attempt crosses BEECON_LOGIN_MAX_ATTEMPTS from the count it
// read a moment ago, so this method never reads the counter back.
func (r *OperatorRepository) RecordFailedAttempt(ctx context.Context, id access.OperatorID, lockedUntil *time.Time) error {
	query := r.db.NewUpdate().
		Model((*OperatorRow)(nil)).
		Set("failed_attempts = failed_attempts + 1").
		Where("id = ?", string(id))
	if lockedUntil != nil {
		query = query.Set("locked_until = ?", *lockedUntil)
	}
	_, err := query.Exec(ctx)
	return err
}

// ResetFailedAttempts zeroes id's failed_attempts and clears locked_until
// (Slice 5's Login, on a successful attempt).
func (r *OperatorRepository) ResetFailedAttempts(ctx context.Context, id access.OperatorID) error {
	_, err := r.db.NewUpdate().
		Model((*OperatorRow)(nil)).
		Set("failed_attempts = ?", 0).
		Set("locked_until = NULL").
		Where("id = ?", string(id)).
		Exec(ctx)
	return err
}

func operatorRowFrom(operator access.Operator) OperatorRow {
	return OperatorRow{
		ID:             string(operator.ID),
		Email:          operator.Email,
		PasswordHash:   operator.PasswordHash,
		Status:         string(operator.Status),
		FailedAttempts: operator.FailedAttempts,
		LockedUntil:    operator.LockedUntil,
		CreatedAt:      operator.CreatedAt,
	}
}

func operatorFromRow(row *OperatorRow) access.Operator {
	return access.Operator{
		ID:             access.OperatorID(row.ID),
		Email:          row.Email,
		PasswordHash:   row.PasswordHash,
		Status:         access.OperatorStatus(row.Status),
		FailedAttempts: row.FailedAttempts,
		LockedUntil:    row.LockedUntil,
		CreatedAt:      row.CreatedAt,
	}
}
