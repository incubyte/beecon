// Package filestore is the execution module's real driven FileStore adapter
// (PD22, ADR-0011): local disk, the only backend Phase 2 ships. A real
// deployment moving off a single disk swaps in an S3/Azure-blob adapter
// behind the same execution.FileStore port, unchanged.
package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"beecon/internal/execution"
)

// Local is the execution module's driven FileStore for local disk: every
// stored file is one file under dir, named by its storage key.
type Local struct {
	dir string
}

var _ execution.FileStore = (*Local)(nil)

// NewLocal builds a Local file store rooted at dir (BEECON_FILES_DIR),
// creating the directory if it does not already exist.
func NewLocal(dir string) (*Local, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("BEECON_FILES_DIR is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create files directory %q: %w", dir, err)
	}
	return &Local{dir: dir}, nil
}

// Save writes content to disk under storageKey, replacing any existing file
// there.
func (l *Local) Save(_ context.Context, storageKey string, content io.Reader) error {
	path, err := l.pathFor(storageKey)
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, content)
	return err
}

// Open returns a stream over storageKey's stored bytes; the caller must
// close it.
func (l *Local) Open(_ context.Context, storageKey string) (io.ReadCloser, error) {
	path, err := l.pathFor(storageKey)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

// Delete removes storageKey's stored file. A file that is already gone is
// not an error.
func (l *Local) Delete(_ context.Context, storageKey string) error {
	path, err := l.pathFor(storageKey)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// pathFor resolves storageKey to a path under dir, rejecting anything that
// could escape it — storage keys are always minted by this module (they are
// FileID values), never taken directly from an untrusted caller, but this is
// cheap defense in depth against a mistake elsewhere.
func (l *Local) pathFor(storageKey string) (string, error) {
	if storageKey == "" || strings.ContainsAny(storageKey, `/\`) || strings.Contains(storageKey, "..") {
		return "", fmt.Errorf("invalid storage key %q", storageKey)
	}
	return filepath.Join(l.dir, storageKey), nil
}
