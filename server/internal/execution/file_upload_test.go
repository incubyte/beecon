// Package execution_test exercises UploadFile, DownloadFile, and Execute's
// file-typed argument resolution (PD22, ADR-0011, Slice 7) against the
// module's own in-memory Files/FileStore adapters
// (driven/memory/files_repository.go, driven/memory/filestore.go) — the
// hand-written ToolReader/ConnectionReader/ProviderClient/Recorder fakes and
// helpers (activeConnectionReader, fakeProviderClient, fakeRecorder,
// fixedClock, assertDomainError, messagesResponse) come from facade_test.go,
// same package.
package execution_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/execution"
	"beecon/internal/execution/driven/memory"
	"beecon/internal/organizations"
)

// sequentialFileIDs mints deterministic, distinct file_-prefixed ids across
// calls — a test-only stand-in for idgen.Prefixed("file_") so assertions
// don't depend on the real CUID2 shape.
func sequentialFileIDs() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("file_test_%d", n)
	}
}

// newFacadeWithFiles builds a Facade with no tools/connections/provider
// wired (nil is safe: UploadFile/DownloadFile never touch them), plus Files/
// FileStore memory adapters and the given maxFileBytes (AC3).
func newFacadeWithFiles(maxFileBytes int64) (*execution.Facade, execution.Files, execution.FileStore) {
	files := memory.NewFilesRepository()
	store := memory.NewFileStore()
	f := execution.NewFacade(nil, nil, nil, nil, fixedClock(time.Now())).
		WithFiles(files, store, maxFileBytes, sequentialFileIDs())
	return f, files, store
}

const defaultTestMaxFileBytes = 1024 * 1024

// --- AC1: upload returns {id, name, mimeType, size} ---

func TestUploadFile_ReturnsAFilePrefixedIDNameMimeTypeAndTrueSize(t *testing.T) {
	f, _, _ := newFacadeWithFiles(defaultTestMaxFileBytes)
	const content = "hello beecon file upload"

	uploaded, err := f.UploadFile(context.Background(), testOrg, "greeting.txt", "text/plain", strings.NewReader(content))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(uploaded.ID), "file_") {
		t.Errorf("ID = %q, want it to start with %q", uploaded.ID, "file_")
	}
	if uploaded.Name != "greeting.txt" {
		t.Errorf("Name = %q, want %q", uploaded.Name, "greeting.txt")
	}
	if uploaded.MimeType != "text/plain" {
		t.Errorf("MimeType = %q, want %q", uploaded.MimeType, "text/plain")
	}
	if uploaded.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", uploaded.Size, len(content))
	}
}

func TestUploadFile_PersistsOrgScopedMetadataFindableAfterward(t *testing.T) {
	f, files, _ := newFacadeWithFiles(defaultTestMaxFileBytes)

	uploaded, err := f.UploadFile(context.Background(), testOrg, "a.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metadata, err := files.FindByID(context.Background(), testOrg, uploaded.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if metadata == nil {
		t.Fatal("FindByID returned nil, want the just-uploaded file's metadata")
	}
	if metadata.OrgID != testOrg {
		t.Errorf("OrgID = %q, want %q", metadata.OrgID, testOrg)
	}
	if metadata.Name != "a.txt" {
		t.Errorf("Name = %q, want %q", metadata.Name, "a.txt")
	}
}

// --- AC3: oversize upload rejected, no real 20 MB allocation needed ---

func TestUploadFile_ContentExceedingTheConfiguredMaxIsRejectedAsAValidationError(t *testing.T) {
	f, _, _ := newFacadeWithFiles(10)

	_, err := f.UploadFile(context.Background(), testOrg, "big.bin", "application/octet-stream", strings.NewReader(strings.Repeat("x", 25)))

	de := assertDomainError(t, err, execution.CodeValidationFailed, http.StatusUnprocessableEntity)
	if de.Status != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want %d", de.Status, http.StatusUnprocessableEntity)
	}
}

func TestUploadFile_ContentExactlyAtTheMaxIsAccepted(t *testing.T) {
	f, _, _ := newFacadeWithFiles(10)

	uploaded, err := f.UploadFile(context.Background(), testOrg, "exact.bin", "application/octet-stream", strings.NewReader(strings.Repeat("x", 10)))

	if err != nil {
		t.Fatalf("unexpected error for content exactly at the max: %v", err)
	}
	if uploaded.Size != 10 {
		t.Errorf("Size = %d, want %d", uploaded.Size, 10)
	}
}

// TestUploadFile_OversizeUploadNeverPersistsMetadata proves an oversize
// upload never leaves a Files row behind — only its (deleted) partial bytes
// were ever written.
func TestUploadFile_OversizeUploadNeverPersistsMetadata(t *testing.T) {
	f, files, _ := newFacadeWithFiles(5)

	_, err := f.UploadFile(context.Background(), testOrg, "big.bin", "application/octet-stream", strings.NewReader("way too much content"))
	if err == nil {
		t.Fatal("expected the oversize upload to fail")
	}

	metadata, findErr := files.FindByID(context.Background(), testOrg, "file_test_1")
	if findErr != nil {
		t.Fatalf("FindByID: %v", findErr)
	}
	if metadata != nil {
		t.Errorf("FindByID returned %+v, want nil — an oversize upload must never persist metadata", metadata)
	}
}

// TestUploadFile_OversizeUploadDeletesItsPartiallyWrittenBytes is AC3's
// cleanup half: the partial write to FileStore must not survive rejection.
func TestUploadFile_OversizeUploadDeletesItsPartiallyWrittenBytes(t *testing.T) {
	f, _, store := newFacadeWithFiles(5)

	_, err := f.UploadFile(context.Background(), testOrg, "big.bin", "application/octet-stream", strings.NewReader("way too much content"))
	if err == nil {
		t.Fatal("expected the oversize upload to fail")
	}

	if _, openErr := store.Open(context.Background(), "file_test_1"); openErr == nil {
		t.Error("FileStore still has bytes under the rejected upload's storage key, want them deleted")
	}
}

// --- AC2: download is org-scoped ---

func TestDownloadFile_ReturnsTheStoredMetadataAndContentForTheOwningOrganization(t *testing.T) {
	f, _, _ := newFacadeWithFiles(defaultTestMaxFileBytes)
	const content = "download me"
	uploaded, err := f.UploadFile(context.Background(), testOrg, "d.txt", "text/plain", strings.NewReader(content))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	metadata, stream, err := f.DownloadFile(context.Background(), testOrg, uploaded.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer stream.Close()
	got, _ := io.ReadAll(stream)
	if string(got) != content {
		t.Errorf("downloaded content = %q, want %q", string(got), content)
	}
	if metadata.Name != "d.txt" {
		t.Errorf("metadata.Name = %q, want %q", metadata.Name, "d.txt")
	}
}

func TestDownloadFile_AnotherOrganizationsRequestForTheSameIDIsNotFound(t *testing.T) {
	f, _, _ := newFacadeWithFiles(defaultTestMaxFileBytes)
	uploaded, err := f.UploadFile(context.Background(), testOrg, "d.txt", "text/plain", strings.NewReader("secret"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	_, _, err = f.DownloadFile(context.Background(), otherOrg, uploaded.ID)

	assertDomainError(t, err, execution.CodeNotFound, http.StatusNotFound)
}

func TestDownloadFile_AnUnknownIDIsNotFound(t *testing.T) {
	f, _, _ := newFacadeWithFiles(defaultTestMaxFileBytes)

	_, _, err := f.DownloadFile(context.Background(), testOrg, "file_never_uploaded")

	assertDomainError(t, err, execution.CodeNotFound, http.StatusNotFound)
}

// --- AC4/AC5/AC6: Execute resolving a file-typed argument ---

// toolWithFileInput is testTool() (facade_test.go) re-shaped as a POST tool
// declaring "file" as a file-typed mapping input — the shape
// hubspot-upload-file's real definition uses (PD22).
func toolWithFileInput() catalog.ProviderTool {
	return catalog.ProviderTool{
		Slug:   testToolSlug,
		Method: "POST",
		Path:   "https://api.hubapi.com/files/v3/files",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file": map[string]any{"type": "string"},
			},
			"required": []any{"file"},
		},
		Mapping: catalog.Mapping{FileInputs: []string{"file"}},
	}
}

// fileExecutionFixture wires a facade with a file-typed tool and its own
// Files/FileStore memory adapters, so tests can upload a file then execute
// the tool against its id.
type fileExecutionFixture struct {
	facade   *execution.Facade
	provider *fakeProviderClient
	recorder *fakeRecorder
}

func newFileExecutionFixture() fileExecutionFixture {
	provider := &fakeProviderClient{response: execution.ToolCallResponse{StatusCode: 200, Body: `{"id":"provider-file-1"}`}}
	recorder := &fakeRecorder{}
	files := memory.NewFilesRepository()
	store := memory.NewFileStore()
	facade := execution.NewFacade(fakeToolReaderWithTool(toolWithFileInput()), activeConnectionReader(), provider, recorder, fixedClock(time.Now())).
		WithFiles(files, store, defaultTestMaxFileBytes, sequentialFileIDs())
	return fileExecutionFixture{facade: facade, provider: provider, recorder: recorder}
}

func (fx fileExecutionFixture) uploadUnder(t *testing.T, org organizations.OrgID, name, mimeType, content string) execution.UploadedFile {
	t.Helper()
	uploaded, err := fx.facade.UploadFile(context.Background(), org, name, mimeType, strings.NewReader(content))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return uploaded
}

func TestExecute_ResolvesAFileTypedArgumentAndSendsItsStoredBytesToTheProvider(t *testing.T) {
	fx := newFileExecutionFixture()
	const content = "the exact bytes the provider must receive"
	uploaded := fx.uploadUnder(t, testOrg, "report.pdf", "application/pdf", content)

	result, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": string(uploaded.ID)})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Successful {
		t.Fatalf("Successful = false, want true; Error = %+v", result.Error)
	}
	if len(fx.provider.lastReq.Files) != 1 {
		t.Fatalf("provider received %d files, want exactly 1", len(fx.provider.lastReq.Files))
	}
	got := fx.provider.lastReq.Files[0]
	if got.FieldName != "file" {
		t.Errorf("FieldName = %q, want %q", got.FieldName, "file")
	}
	if got.FileName != "report.pdf" {
		t.Errorf("FileName = %q, want %q", got.FileName, "report.pdf")
	}
	if got.MimeType != "application/pdf" {
		t.Errorf("MimeType = %q, want %q", got.MimeType, "application/pdf")
	}
	if string(got.Content) != content {
		t.Errorf("Content = %q, want %q", string(got.Content), content)
	}
	if got.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", got.Size, len(content))
	}
}

// TestExecute_DoesNotForwardTheFileTypedArgumentAsALiteralQueryValue proves
// withoutFileInputs keeps the file_ id itself out of the generic query
// pass-through — the id names a stored file, never a literal value for the
// provider's own query string.
func TestExecute_DoesNotForwardTheFileTypedArgumentAsALiteralQueryValue(t *testing.T) {
	fx := newFileExecutionFixture()
	uploaded := fx.uploadUnder(t, testOrg, "report.pdf", "application/pdf", "content")

	_, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": string(uploaded.ID)})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := fx.provider.lastReq.Query["file"]; present {
		t.Errorf(`Query["file"] = %q, want the file-typed argument omitted from the query pass-through entirely`, fx.provider.lastReq.Query["file"])
	}
}

// --- AC5: unknown/cross-org file_ id never reaches the provider ---

func TestExecute_AnUnknownFileIDReturnsFileNotFoundAndNeverCallsTheProvider(t *testing.T) {
	fx := newFileExecutionFixture()

	result, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": "file_does_not_exist"})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v (a file-typed resolution failure is a tool-level failure)", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for an unknown file_ id")
	}
	if result.Error == nil || result.Error.Code != execution.CodeFileNotFound {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeFileNotFound)
	}
	if fx.provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0 — an unresolved file_ id must never reach the provider", fx.provider.callCount)
	}
}

func TestExecute_ACrossOrganizationFileIDReturnsFileNotFoundAndNeverCallsTheProvider(t *testing.T) {
	fx := newFileExecutionFixture()
	uploaded := fx.uploadUnder(t, otherOrg, "not-yours.txt", "text/plain", "content")

	result, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": string(uploaded.ID)})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for a cross-organization file_ id")
	}
	if result.Error == nil || result.Error.Code != execution.CodeFileNotFound {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeFileNotFound)
	}
	if fx.provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0 — a cross-organization file_ id must never reach the provider", fx.provider.callCount)
	}
}

// TestExecute_AnEmptyFileTypedArgumentReturnsFileNotFoundWithoutCallingTheProvider
// covers resolveOneFileInput's other rejection branch: a syntactically valid
// (schema-passing) but empty string can never name a real uploaded file, so
// it is reported the same way an unresolvable id is, and the provider is
// still never called.
func TestExecute_AnEmptyFileTypedArgumentReturnsFileNotFoundWithoutCallingTheProvider(t *testing.T) {
	fx := newFileExecutionFixture()

	result, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": ""})

	if err != nil {
		t.Fatalf("unexpected platform-level error: %v", err)
	}
	if result.Successful {
		t.Fatal("Successful = true, want false for an empty file-typed argument")
	}
	if result.Error == nil || result.Error.Code != execution.CodeFileNotFound {
		t.Fatalf("Error = %+v, want code %q", result.Error, execution.CodeFileNotFound)
	}
	if fx.provider.callCount != 0 {
		t.Errorf("provider was called %d times, want 0", fx.provider.callCount)
	}
}

// --- AC6: file bytes never reach a log entry ---

func TestExecute_LogsTheFileFieldNameIDAndSizeButNeverItsBytes(t *testing.T) {
	fx := newFileExecutionFixture()
	const secretContent = "THESE-EXACT-BYTES-MUST-NEVER-APPEAR-IN-A-LOG-ENTRY"
	uploaded := fx.uploadUnder(t, testOrg, "secret.txt", "text/plain", secretContent)

	_, err := fx.facade.Execute(context.Background(), testOrg, testUser, testConnectionID, testToolSlug, map[string]any{"file": string(uploaded.ID)})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fx.recorder.entries) != 1 {
		t.Fatalf("recorded %d entries, want exactly 1", len(fx.recorder.entries))
	}
	entry := fx.recorder.entries[0]
	if strings.Contains(entry.RequestBody, secretContent) {
		t.Fatalf("logged RequestBody contains the raw file bytes: %s", entry.RequestBody)
	}
	if !strings.Contains(entry.RequestBody, string(uploaded.ID)) {
		t.Errorf("logged RequestBody %q does not carry the file id %q", entry.RequestBody, uploaded.ID)
	}
	if !strings.Contains(entry.RequestBody, fmt.Sprintf(`"size":%d`, len(secretContent))) {
		t.Errorf("logged RequestBody %q does not carry the file size %d", entry.RequestBody, len(secretContent))
	}
	if !strings.Contains(entry.RequestBody, `"fieldName":"file"`) {
		t.Errorf("logged RequestBody %q does not carry the fieldName", entry.RequestBody)
	}
}
