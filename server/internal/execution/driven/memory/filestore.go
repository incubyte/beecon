// Package memory holds the execution module's in-memory driven adapters for
// tests: a FileStore that never touches disk, and a Files metadata
// repository.
package memory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	"beecon/internal/execution"
)

// FileStore is an in-memory execution.FileStore for tests.
type FileStore struct {
	mu   sync.RWMutex
	byID map[string][]byte
}

var _ execution.FileStore = (*FileStore)(nil)

func NewFileStore() *FileStore {
	return &FileStore{byID: map[string][]byte{}}
}

func (s *FileStore) Save(_ context.Context, storageKey string, content io.Reader) error {
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[storageKey] = data
	return nil
}

func (s *FileStore) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.byID[storageKey]
	if !ok {
		return nil, errors.New("storage key not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *FileStore) Delete(_ context.Context, storageKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, storageKey)
	return nil
}
