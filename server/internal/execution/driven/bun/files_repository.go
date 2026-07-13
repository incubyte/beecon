// Package bun is the execution module's persistence adapter for its Files
// metadata repository. It is the only place in the module that imports
// database/sql or uptrace/bun; the row struct's bun tags are the schema's
// source of truth.
package bun

import (
	"context"
	"database/sql"
	"errors"
	"time"

	upstreambun "github.com/uptrace/bun"

	"beecon/internal/execution"
	"beecon/internal/organizations"
)

// FileRow is the files table schema.
type FileRow struct {
	upstreambun.BaseModel `bun:"table:files,alias:f"`

	ID         string    `bun:"id,pk"`
	OrgID      string    `bun:"org_id,notnull"`
	Name       string    `bun:"name,notnull"`
	MimeType   string    `bun:"mime_type,notnull"`
	Size       int64     `bun:"size,notnull"`
	StorageKey string    `bun:"storage_key,notnull"`
	CreatedAt  time.Time `bun:"created_at,notnull"`
}

// FilesRepository is the bun-backed execution.Files.
type FilesRepository struct {
	db *upstreambun.DB
}

var _ execution.Files = (*FilesRepository)(nil)

func NewFilesRepository(db *upstreambun.DB) *FilesRepository {
	return &FilesRepository{db: db}
}

func (r *FilesRepository) Save(ctx context.Context, file execution.FileMetadata) error {
	row := fileRowFrom(file)
	_, err := r.db.NewInsert().Model(&row).Exec(ctx)
	return err
}

func (r *FilesRepository) FindByID(ctx context.Context, org organizations.OrgID, id execution.FileID) (*execution.FileMetadata, error) {
	row := new(FileRow)
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
	file := fileFromRow(row)
	return &file, nil
}

func fileRowFrom(file execution.FileMetadata) FileRow {
	return FileRow{
		ID:         string(file.ID),
		OrgID:      string(file.OrgID),
		Name:       file.Name,
		MimeType:   file.MimeType,
		Size:       file.Size,
		StorageKey: file.StorageKey,
		CreatedAt:  file.CreatedAt,
	}
}

func fileFromRow(row *FileRow) execution.FileMetadata {
	return execution.FileMetadata{
		ID:         execution.FileID(row.ID),
		OrgID:      organizations.OrgID(row.OrgID),
		Name:       row.Name,
		MimeType:   row.MimeType,
		Size:       row.Size,
		StorageKey: row.StorageKey,
		CreatedAt:  row.CreatedAt,
	}
}
