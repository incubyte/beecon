// files.go is Slice 7's FileUpload domain (PD22, ADR-0011): a Beecon-minted
// file travels from an HTTP multipart upload to a tool call's file-typed
// argument, entirely behind the FileStore byte-storage port. There is no
// auto-expiry or retention in Phase 2 — a file lives until something else
// deletes it, which nothing does yet.
package execution

import (
	"io"
	"time"

	"beecon/internal/organizations"
)

// FileID is minted only by UploadFile ("file_" prefix).
type FileID string

// FileMetadata is one uploaded file's record (AC1). OrgID scopes every
// lookup — a cross-organization id is never found (AC2, AC5), the same
// isolation rule every other org-scoped entity already follows. StorageKey
// is the opaque handle FileStore uses to find the bytes; it never appears in
// an API response.
type FileMetadata struct {
	ID         FileID
	OrgID      organizations.OrgID
	Name       string
	MimeType   string
	Size       int64
	StorageKey string
	CreatedAt  time.Time
}

// UploadedFile is what UploadFile returns (AC1): everything a consumer needs
// to reference the file again — as a download link, or as a file-typed
// argument in a later tool call.
type UploadedFile struct {
	ID       FileID
	Name     string
	MimeType string
	Size     int64
}

// countingReader wraps an io.Reader and tracks how many bytes have passed
// through it, so UploadFile can measure a file's true size as it streams to
// storage (AC3) without ever buffering the whole file first to find out.
type countingReader struct {
	reader io.Reader
	count  int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.count += int64(n)
	return n, err
}
