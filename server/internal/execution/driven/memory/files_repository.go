package memory

import (
	"context"
	"sync"

	"beecon/internal/execution"
	"beecon/internal/organizations"
)

// FilesRepository is an in-memory execution.Files for tests.
type FilesRepository struct {
	mu   sync.RWMutex
	byID map[execution.FileID]execution.FileMetadata
}

var _ execution.Files = (*FilesRepository)(nil)

func NewFilesRepository() *FilesRepository {
	return &FilesRepository{byID: map[execution.FileID]execution.FileMetadata{}}
}

func (r *FilesRepository) Save(_ context.Context, file execution.FileMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[file.ID] = file
	return nil
}

func (r *FilesRepository) FindByID(_ context.Context, org organizations.OrgID, id execution.FileID) (*execution.FileMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	file, ok := r.byID[id]
	if !ok || file.OrgID != org {
		return nil, nil
	}
	copied := file
	return &copied, nil
}
