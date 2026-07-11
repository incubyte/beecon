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

	ID            string    `bun:"id,pk"`
	OrgID         string    `bun:"org_id,notnull"`
	UserID        string    `bun:"user_id,notnull"`
	IntegrationID string    `bun:"integration_id,notnull"`
	ProviderSlug  string    `bun:"provider_slug,notnull"`
	Status        string    `bun:"status,notnull"`
	RedirectURI   string    `bun:"redirect_uri,notnull"`
	ConnectToken  string    `bun:"connect_token,notnull"`
	CreatedAt     time.Time `bun:"created_at,notnull"`
}

// Repository is the bun-backed connections.Repository.
type Repository struct {
	db *upstreambun.DB
}

var _ connections.Repository = (*Repository)(nil)

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

func rowFromConnection(connection connections.Connection) ConnectionRow {
	return ConnectionRow{
		ID:            string(connection.ID),
		OrgID:         string(connection.OrgID),
		UserID:        string(connection.UserID),
		IntegrationID: string(connection.IntegrationID),
		ProviderSlug:  connection.ProviderSlug,
		Status:        string(connection.Status),
		RedirectURI:   connection.RedirectURI,
		ConnectToken:  connection.ConnectToken,
		CreatedAt:     connection.CreatedAt,
	}
}

func connectionFromRow(row *ConnectionRow) connections.Connection {
	return connections.Connection{
		ID:            connections.ConnectionID(row.ID),
		OrgID:         organizations.OrgID(row.OrgID),
		UserID:        organizations.UserID(row.UserID),
		IntegrationID: catalog.IntegrationID(row.IntegrationID),
		ProviderSlug:  row.ProviderSlug,
		Status:        connections.Status(row.Status),
		RedirectURI:   row.RedirectURI,
		ConnectToken:  row.ConnectToken,
		CreatedAt:     row.CreatedAt,
	}
}
