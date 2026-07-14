// Package bun is the connections module's persistence adapter. It is the
// only place in the module that imports database/sql or uptrace/bun; the row
// struct's bun tags are the schema's source of truth.
package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// ConnectionRow is the connections table schema. RefreshLeaseUntil/
// ReconcileLeaseUntil are never set by Update — only the raw
// ClaimDueRefresh/ClaimDueReconcile queries write a real lease.
type ConnectionRow struct {
	upstreambun.BaseModel `bun:"table:connections,alias:c"`

	ID                    string     `bun:"id,pk"`
	OrgID                 string     `bun:"org_id,notnull"`
	UserID                string     `bun:"user_id,notnull"`
	IntegrationID         string     `bun:"integration_id,notnull"`
	ProviderSlug          string     `bun:"provider_slug,notnull"`
	Status                string     `bun:"status,notnull"`
	RedirectURI           string     `bun:"redirect_uri,notnull"`
	ConnectToken          string     `bun:"connect_token,notnull"`
	ConnectTokenExpiresAt time.Time  `bun:"connect_token_expires_at,notnull"`
	ConnectTokenUsed      bool       `bun:"connect_token_used,notnull"`
	EncryptedAccessToken  string     `bun:"encrypted_access_token,notnull"`
	EncryptedRefreshToken string     `bun:"encrypted_refresh_token,notnull"`
	TokenExpiresAt        *time.Time `bun:"token_expires_at"`
	AccountEmail          string     `bun:"account_email,notnull"`
	AccountDisplayName    string     `bun:"account_display_name,notnull"`
	EncryptedParams       string     `bun:"encrypted_params,notnull"`
	ReconciledAt          *time.Time `bun:"reconciled_at"`
	RefreshLeaseUntil     *time.Time `bun:"refresh_lease_until"`
	ReconcileLeaseUntil   *time.Time `bun:"reconcile_lease_until"`
	CreatedAt             time.Time  `bun:"created_at,notnull"`
}

// claimDueRefreshPostgres/claimDueRefreshSQLite are the dual-dialect claim
// primitive for PD36's refresh scheduler: a nil token_expires_at (a
// Phase-1-migrated row) is also due.
const claimDueRefreshPostgres = `
UPDATE connections
SET refresh_lease_until = ?
WHERE id IN (
	SELECT id FROM connections
	WHERE status = ? AND (token_expires_at IS NULL OR token_expires_at <= ?)
		AND (refresh_lease_until IS NULL OR refresh_lease_until < ?)
	ORDER BY created_at
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
RETURNING *
`

const claimDueRefreshSQLite = `
UPDATE connections
SET refresh_lease_until = ?
WHERE id IN (
	SELECT id FROM connections
	WHERE status = ? AND (token_expires_at IS NULL OR token_expires_at <= ?)
		AND (refresh_lease_until IS NULL OR refresh_lease_until < ?)
	ORDER BY created_at
	LIMIT ?
)
RETURNING *
`

// claimDueReconcilePostgres/claimDueReconcileSQLite: PD37's reconciliation
// job — a nil reconciled_at (never yet reconciled) is also due immediately.
const claimDueReconcilePostgres = `
UPDATE connections
SET reconcile_lease_until = ?
WHERE id IN (
	SELECT id FROM connections
	WHERE status = ? AND (reconciled_at IS NULL OR reconciled_at <= ?)
		AND (reconcile_lease_until IS NULL OR reconcile_lease_until < ?)
	ORDER BY created_at
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
RETURNING *
`

const claimDueReconcileSQLite = `
UPDATE connections
SET reconcile_lease_until = ?
WHERE id IN (
	SELECT id FROM connections
	WHERE status = ? AND (reconciled_at IS NULL OR reconciled_at <= ?)
		AND (reconcile_lease_until IS NULL OR reconcile_lease_until < ?)
	ORDER BY created_at
	LIMIT ?
)
RETURNING *
`

// OAuthStateRow is the oauth_states table schema.
type OAuthStateRow struct {
	upstreambun.BaseModel `bun:"table:oauth_states,alias:os"`

	State        string     `bun:"state,pk"`
	ConnectionID string     `bun:"connection_id,notnull"`
	ExpiresAt    time.Time  `bun:"expires_at,notnull"`
	ConsumedAt   *time.Time `bun:"consumed_at"`
}

// Repository is the bun-backed connections.Repository,
// connections.OAuthRepository, and connections.RefreshQueue.
type Repository struct {
	db *upstreambun.DB
}

var _ connections.Repository = (*Repository)(nil)
var _ connections.OAuthRepository = (*Repository)(nil)
var _ connections.RefreshQueue = (*Repository)(nil)
var _ connections.StatusCounter = (*Repository)(nil)

func NewRepository(db *upstreambun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(ctx context.Context, connection connections.Connection) error {
	row := rowFromConnection(connection)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) FindByID(ctx context.Context, org organizations.OrgID, id connections.ConnectionID) (*connections.Connection, error) {
	row := new(ConnectionRow)
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
	connection := connectionFromRow(row)
	return &connection, nil
}

// Update persists a Connection's mutable fields, including reconciled_at
// (PD37); refresh_lease_until/reconcile_lease_until are always overwritten to
// NULL, releasing whatever claim lease a prior ClaimDueRefresh/ClaimDueReconcile
// set.
func (r *Repository) Update(ctx context.Context, connection connections.Connection) error {
	row := rowFromConnection(connection)
	_, err := r.db.NewUpdate().
		Model(&row).
		Column(
			"status",
			"redirect_uri",
			"connect_token",
			"connect_token_expires_at",
			"connect_token_used",
			"encrypted_access_token",
			"encrypted_refresh_token",
			"token_expires_at",
			"account_email",
			"account_display_name",
			"encrypted_params",
			"reconciled_at",
			"refresh_lease_until",
			"reconcile_lease_until",
		).
		Where("id = ?", row.ID).
		Exec(ctx)
	return err
}

// TransitionStatus conditionally flips id's status from -> to, reporting
// whether this call performed the flip (FD1).
func (r *Repository) TransitionStatus(ctx context.Context, org organizations.OrgID, id connections.ConnectionID, from, to connections.Status) (bool, error) {
	result, err := r.db.NewUpdate().
		Model((*ConnectionRow)(nil)).
		Set("status = ?", string(to)).
		Where("id = ?", string(id)).
		Where("org_id = ?", string(org)).
		Where("status = ?", string(from)).
		Exec(ctx)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// ClaimDueRefresh leases up to limit due connections, oldest-created first.
func (r *Repository) ClaimDueRefresh(ctx context.Context, now time.Time, lead, leaseTTL time.Duration, limit int) ([]connections.Connection, error) {
	query := claimDueRefreshSQLite
	if r.db.Dialect().Name() == dialect.PG {
		query = claimDueRefreshPostgres
	}
	leaseUntil := now.Add(leaseTTL)
	dueBy := now.Add(lead)

	var rows []ConnectionRow
	if err := r.db.NewRaw(query, leaseUntil, string(connections.StatusActive), dueBy, now, limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return connectionsFromRows(rows), nil
}

// ClaimDueReconcile leases up to limit due connections, oldest-created first.
func (r *Repository) ClaimDueReconcile(ctx context.Context, now time.Time, interval, leaseTTL time.Duration, limit int) ([]connections.Connection, error) {
	query := claimDueReconcileSQLite
	if r.db.Dialect().Name() == dialect.PG {
		query = claimDueReconcilePostgres
	}
	leaseUntil := now.Add(leaseTTL)
	dueBefore := now.Add(-interval)

	var rows []ConnectionRow
	if err := r.db.NewRaw(query, leaseUntil, string(connections.StatusActive), dueBefore, now, limit).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	return connectionsFromRows(rows), nil
}

// statusCountRow is the scan target for the connections-by-status metrics
// gauge's GROUP BY query (PD38d).
type statusCountRow struct {
	Status string `bun:"status"`
	Count  int    `bun:"count"`
}

// CountByStatus returns the number of connections currently in each
// lifecycle status across every organization in the installation (PD38d) —
// deliberately not org-scoped (connections.StatusCounter's own doc comment):
// a metrics gauge is an installation-wide signal.
func (r *Repository) CountByStatus(ctx context.Context) (map[connections.Status]int, error) {
	var rows []statusCountRow
	err := r.db.NewSelect().
		Model((*ConnectionRow)(nil)).
		ColumnExpr("status AS status").
		ColumnExpr("COUNT(*) AS count").
		Group("status").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	counts := make(map[connections.Status]int, len(rows))
	for _, row := range rows {
		counts[connections.Status(row.Status)] = row.Count
	}
	return counts, nil
}

func connectionsFromRows(rows []ConnectionRow) []connections.Connection {
	results := make([]connections.Connection, 0, len(rows))
	for _, row := range rows {
		results = append(results, connectionFromRow(&row))
	}
	return results
}

// List returns Connections scoped to org (Slice 4, AC1), optionally
// narrowed to filter.UserID, newest first (created_at DESC, id DESC as a
// deterministic tiebreaker), limited to filter.Limit rows.
func (r *Repository) List(ctx context.Context, org organizations.OrgID, filter connections.ListFilter) ([]connections.Connection, error) {
	var rows []ConnectionRow
	query := r.db.NewSelect().Model(&rows).Where("org_id = ?", string(org))

	if filter.UserID != "" {
		query = query.Where("user_id = ?", string(filter.UserID))
	}
	if filter.Cursor != nil {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))",
			filter.Cursor.CreatedAt, filter.Cursor.CreatedAt, string(filter.Cursor.ID))
	}

	err := query.
		Order("created_at DESC", "id DESC").
		Limit(filter.Limit).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]connections.Connection, 0, len(rows))
	for _, row := range rows {
		results = append(results, connectionFromRow(&row))
	}
	return results, nil
}

// Delete permanently removes the row for id scoped to org (Slice 4, AC3):
// a hard delete, so its encrypted credentials are destroyed along with it. A
// cross-org or unknown id affects zero rows — the facade has already turned
// that into ErrNotFound via a preceding FindByID.
func (r *Repository) Delete(ctx context.Context, org organizations.OrgID, id connections.ConnectionID) error {
	_, err := r.db.NewDelete().
		Model((*ConnectionRow)(nil)).
		Where("id = ?", string(id)).
		Where("org_id = ?", string(org)).
		Exec(ctx)
	return err
}

// FindByConnectToken looks up a Connection by its single-use connect token,
// with no organization filter — the connect page authenticates through this
// token before any organization is known.
func (r *Repository) FindByConnectToken(ctx context.Context, token string) (*connections.Connection, error) {
	row := new(ConnectionRow)
	err := r.db.NewSelect().
		Model(row).
		Where("connect_token = ?", token).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	connection := connectionFromRow(row)
	return &connection, nil
}

// FindConnectionForCallback looks up a Connection by id with no organization
// filter: the OAuth callback only reaches this after already validating the
// CSRF state that names this exact id, so the state itself is the proof of
// authorized access.
func (r *Repository) FindConnectionForCallback(ctx context.Context, id connections.ConnectionID) (*connections.Connection, error) {
	row := new(ConnectionRow)
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
	connection := connectionFromRow(row)
	return &connection, nil
}

func (r *Repository) SaveState(ctx context.Context, state connections.OAuthState) error {
	row := OAuthStateRow{
		State:        state.State,
		ConnectionID: string(state.ConnectionID),
		ExpiresAt:    state.ExpiresAt,
		ConsumedAt:   state.ConsumedAt,
	}
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *Repository) FindState(ctx context.Context, state string) (*connections.OAuthState, error) {
	row := new(OAuthStateRow)
	err := r.db.NewSelect().
		Model(row).
		Where("state = ?", state).
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	result := &connections.OAuthState{
		State:        row.State,
		ConnectionID: connections.ConnectionID(row.ConnectionID),
		ExpiresAt:    row.ExpiresAt,
		ConsumedAt:   row.ConsumedAt,
	}
	return result, nil
}

// MarkStateConsumed marks state consumed via a compare-and-set update
// (WHERE consumed_at IS NULL): if two callbacks race on the same state, only
// the first update affects a row. The second sees zero rows affected and
// gets ErrStateAlreadyUsed, so a state can never be consumed twice even
// under concurrent callbacks.
func (r *Repository) MarkStateConsumed(ctx context.Context, state string, consumedAt time.Time) error {
	row := OAuthStateRow{State: state, ConsumedAt: &consumedAt}
	result, err := r.db.NewUpdate().
		Model(&row).
		Column("consumed_at").
		Where("state = ?", state).
		Where("consumed_at IS NULL").
		Exec(ctx)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return connections.ErrStateAlreadyUsed()
	}
	return nil
}

// rowFromConnection always leaves RefreshLeaseUntil/ReconcileLeaseUntil nil —
// only the raw claim queries ever write a real lease.
func rowFromConnection(connection connections.Connection) ConnectionRow {
	return ConnectionRow{
		ID:                    string(connection.ID),
		OrgID:                 string(connection.OrgID),
		UserID:                string(connection.UserID),
		IntegrationID:         string(connection.IntegrationID),
		ProviderSlug:          connection.ProviderSlug,
		Status:                string(connection.Status),
		RedirectURI:           connection.RedirectURI,
		ConnectToken:          connection.ConnectToken,
		ConnectTokenExpiresAt: connection.ConnectTokenExpiresAt,
		ConnectTokenUsed:      connection.ConnectTokenUsed,
		EncryptedAccessToken:  connection.EncryptedAccessToken,
		EncryptedRefreshToken: connection.EncryptedRefreshToken,
		TokenExpiresAt:        connection.TokenExpiresAt,
		AccountEmail:          connection.AccountEmail,
		AccountDisplayName:    connection.AccountDisplayName,
		EncryptedParams:       connection.EncryptedParams,
		ReconciledAt:          connection.ReconciledAt,
		RefreshLeaseUntil:     nil,
		ReconcileLeaseUntil:   nil,
		CreatedAt:             connection.CreatedAt,
	}
}

func connectionFromRow(row *ConnectionRow) connections.Connection {
	return connections.Connection{
		ID:                    connections.ConnectionID(row.ID),
		OrgID:                 organizations.OrgID(row.OrgID),
		UserID:                organizations.UserID(row.UserID),
		IntegrationID:         catalog.IntegrationID(row.IntegrationID),
		ProviderSlug:          row.ProviderSlug,
		Status:                connections.Status(row.Status),
		RedirectURI:           row.RedirectURI,
		ConnectToken:          row.ConnectToken,
		ConnectTokenExpiresAt: row.ConnectTokenExpiresAt,
		ConnectTokenUsed:      row.ConnectTokenUsed,
		EncryptedAccessToken:  row.EncryptedAccessToken,
		EncryptedRefreshToken: row.EncryptedRefreshToken,
		TokenExpiresAt:        row.TokenExpiresAt,
		AccountEmail:          row.AccountEmail,
		AccountDisplayName:    row.AccountDisplayName,
		EncryptedParams:       row.EncryptedParams,
		ReconciledAt:          row.ReconciledAt,
		CreatedAt:             row.CreatedAt,
	}
}
