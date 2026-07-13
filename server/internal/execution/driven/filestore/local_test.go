// Package filestore_test exercises the local-disk FileStore adapter (PD22,
// ADR-0011) against a real temp directory: NewLocal's directory-creation and
// missing-dir validation, a save/open round trip, overwrite semantics,
// idempotent delete, and the storage-key path-traversal defense.
package filestore_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"beecon/internal/execution/driven/filestore"
)

func TestNewLocal_RejectsAnEmptyDirectory(t *testing.T) {
	_, err := filestore.NewLocal("")

	if err == nil {
		t.Fatal("expected an error for an empty BEECON_FILES_DIR, got nil")
	}
	if !strings.Contains(err.Error(), "BEECON_FILES_DIR") {
		t.Errorf("error = %q, want it to name BEECON_FILES_DIR", err.Error())
	}
}

func TestNewLocal_CreatesTheDirectoryWhenItDoesNotYetExist(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist-yet")

	if _, err := filestore.NewLocal(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		t.Fatalf("stat %q: %v, want NewLocal to have created it", dir, statErr)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", dir)
	}
}

func TestLocal_SaveThenOpenRoundTripsTheExactBytes(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	const content = "the quick brown fox jumps over the lazy dog"

	if err := store.Save(context.Background(), "file_1", strings.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reader, err := store.Open(context.Background(), "file_1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != content {
		t.Errorf("read content = %q, want %q", string(got), content)
	}
}

func TestLocal_SaveOverwritesAPreviouslyStoredFileUnderTheSameKey(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, "file_1", strings.NewReader("first version")); err != nil {
		t.Fatalf("Save (first): %v", err)
	}

	if err := store.Save(ctx, "file_1", strings.NewReader("second version")); err != nil {
		t.Fatalf("Save (second): %v", err)
	}

	reader, err := store.Open(ctx, "file_1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if string(got) != "second version" {
		t.Errorf("content after overwrite = %q, want %q", string(got), "second version")
	}
}

func TestLocal_OpenAnUnknownStorageKeyReturnsAnError(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	_, err = store.Open(context.Background(), "file_never_saved")

	if err == nil {
		t.Fatal("expected an error opening a storage key that was never saved, got nil")
	}
}

func TestLocal_DeleteRemovesTheStoredFile(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, "file_1", bytes.NewBufferString("gone soon")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Delete(ctx, "file_1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.Open(ctx, "file_1"); err == nil {
		t.Fatal("expected Open to fail after Delete, got nil error")
	}
}

func TestLocal_DeleteOfAnAlreadyMissingKeyIsNotAnError(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}

	if err := store.Delete(context.Background(), "file_never_existed"); err != nil {
		t.Errorf("Delete of a missing key returned %v, want nil (idempotent delete)", err)
	}
}

// TestLocal_RejectsStorageKeysThatWouldEscapeTheRootDirectory is cheap
// defense in depth (local.go's own doc comment): storage keys are always
// minted internally as FileID values, but Save/Open/Delete must still refuse
// anything that looks like a path-traversal attempt rather than silently
// resolving it outside dir.
func TestLocal_RejectsStorageKeysThatWouldEscapeTheRootDirectory(t *testing.T) {
	store, err := filestore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	ctx := context.Background()

	maliciousKeys := []string{"../escaped", "a/../../escaped", "sub/dir", `back\slash`, ""}
	for _, key := range maliciousKeys {
		if err := store.Save(ctx, key, strings.NewReader("x")); err == nil {
			t.Errorf("Save(%q) succeeded, want an error rejecting the unsafe storage key", key)
		}
		if _, err := store.Open(ctx, key); err == nil {
			t.Errorf("Open(%q) succeeded, want an error rejecting the unsafe storage key", key)
		}
		if err := store.Delete(ctx, key); err == nil {
			t.Errorf("Delete(%q) succeeded, want an error rejecting the unsafe storage key", key)
		}
	}
}
