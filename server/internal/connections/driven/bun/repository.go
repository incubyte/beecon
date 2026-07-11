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

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
)

// ConnectionRow is the connections table schema.
type ConnectionRow struct {
	upstreambun.BaseModel `bun:"table:connections,alias:c"`

	ID                    string    `bun:"id,pk"`
	OrgID                 string    `bun:"org_id,notnull"`
	UserID                string    `bun:"user_id,notnull"`
	IntegrationID         string    `bun:"integration_id,notnull"`
	ProviderSlug          string    `bun:"provider_slug,notnull"`
	Status                string    `bun:"status,notnull"`
	RedirectURI           string    `bun:"redirect_uri,notnull"`
	ConnectToken          string    `bun:"connect_token,notnull"`
	EncryptedAccessToken  string    `bun:"encrypted_access_token,notnull"`
	EncryptedRefreshToken string    `bun:"encrypted_refresh_token,notnull"`
	AccountEmail          string    `bun:"account_email,notnull"`
	AccountDisplayName    string    `bun:"account_display_name,notnull"`
	CreatedAt             time.Time `bun:"created_at,notnull"`
}

// OAuthStateRow is the oauth_states table schema.
type OAuthStateRow struct {
	upstreambun.BaseModel `bun:"table:oauth_states,alias:os"`

	State        string     `bun:"state,pk"`
	ConnectionID string     `bun:"connection_id,notnull"`
	ExpiresAt    time.Time  `bun:"expires_at,notnull"`
	ConsumedAt   *time.Time `bun:"consumed_at"`
}

// Repository is the bun-backed connections.Repository and
// connections.OAuthRepository.
type Repository struct {
	db *upstreambun.DB
}

var _ connections.Repository = (*Repository)(nil)
var _ connections.OAuthRepository = (*Repository)(nil)

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

// Update persists a previously initiated Connection's mutable fields: status
// and the OAuth callback's activation payload (encrypted tokens, account
// metadata).
func (r *Repository) Update(ctx context.Context, connection connections.Connection) error {
	row := rowFromConnection(connection)
	_, err := r.db.NewUpdate().
		Model(&row).
		Column("status", "encrypted_access_token", "encrypted_refresh_token", "account_email", "account_display_name").
		Where("id = ?", row.ID).
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

func (r *Repository) MarkStateConsumed(ctx context.Context, state string, consumedAt time.Time) error {
	row := OAuthStateRow{State: state, ConsumedAt: &consumedAt}
	_, err := r.db.NewUpdate().
		Model(&row).
		Column("consumed_at").
		Where("state = ?", state).
		Exec(ctx)
	return err
}

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
		EncryptedAccessToken:  connection.EncryptedAccessToken,
		EncryptedRefreshToken: connection.EncryptedRefreshToken,
		AccountEmail:          connection.AccountEmail,
		AccountDisplayName:    connection.AccountDisplayName,
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
		EncryptedAccessToken:  row.EncryptedAccessToken,
		EncryptedRefreshToken: row.EncryptedRefreshToken,
		AccountEmail:          row.AccountEmail,
		AccountDisplayName:    row.AccountDisplayName,
		CreatedAt:             row.CreatedAt,
	}
}
