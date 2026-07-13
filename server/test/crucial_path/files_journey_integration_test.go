//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO,
// wireErrorEnvelope, doJSONRequest, hubspotDefinitionAgainst,
// newHubspotJourneyFixture, activateHubspotConnection, executeHubspotTool,
// executionResultWithCursorDTO from hubspot_journey_integration_test.go;
// issuedSigningSecretDTO, mintBrowserUserToken from
// browser_token_journey_integration_test.go; fetchLogs, logsPageDTO from
// tool_execution_journey_integration_test.go — same package). This file
// tells Slice 7's story end to end against the real composition root and
// FakeHubspot's own /files/v3/files endpoint: upload returns a
// file_-prefixed id, name, mimeType, size, and an org-authenticated
// downloadUrl; another organization's download request is not-found; an
// upload past the configured size limit is rejected without allocating a
// real 20 MB payload; executing hubspot-upload-file with a file_ id streams
// the stored bytes to Hubspot and returns its own file record; an unknown or
// cross-organization file_ id never reaches the provider; the resulting log
// entry carries the file id and size but never its bytes; and a valid user
// token — Slice 5's deferred AC — cannot reach either files endpoint.
package crucial_path

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/test/support"
)

type uploadedFileDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MimeType    string `json:"mimeType"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"downloadUrl"`
}

// doMultipartUpload builds a real multipart/form-data POST body carrying one
// file part named "file" (the field name hubspot-upload-file's own mapping
// and FilesHandler.Upload both expect the request's first file part to be),
// and fires it at handler exactly like a live client would.
func doMultipartUpload(t *testing.T, handler http.Handler, authHeader, fileName, mimeType string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {fmt.Sprintf(`form-data; name="file"; filename=%q`, fileName)},
		"Content-Type":        {mimeType},
	})
	if err != nil {
		t.Fatalf("create multipart part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// uploadFile is doMultipartUpload plus the expected-success decode, for
// tests whose story starts from an already-uploaded file.
func uploadFile(t *testing.T, wired *app.Wired, orgAuth, fileName, mimeType string, content []byte) uploadedFileDTO {
	t.Helper()
	w := doMultipartUpload(t, wired.Router, orgAuth, fileName, mimeType, content)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var uploaded uploadedFileDTO
	if err := json.Unmarshal(w.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode uploaded file: %v; body=%s", err, w.Body.String())
	}
	return uploaded
}

// createSeparateOrgWithKey scaffolds a brand-new organization with its own
// org API key, unrelated to any other fixture in this file — used to prove
// cross-organization isolation (AC2, AC5).
func createSeparateOrgWithKey(t *testing.T, wired *app.Wired, name string) string {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, fmt.Sprintf(`{"name":%q}`, name))
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var key issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	return "Bearer " + key.Key
}

// newFilesUserTokenFixture scaffolds a fresh organization, one user, and one
// signing secret, then mints a valid (unexpired) user token for that user —
// the minimum fixture the deferred Slice 5 AC needs, independent of any
// provider/integration setup.
func newFilesUserTokenFixture(t *testing.T, wired *app.Wired) (orgAuth, userAuth string) {
	t.Helper()
	adminAuth := "Bearer " + support.AdminAPIKey

	var org organizationDTO
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme Files"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatalf("decode org: %v", err)
	}

	var orgKey issuedKeyDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	orgAuth = "Bearer " + orgKey.Key

	var user userDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/users/", orgAuth, `{"name":"Ada Lovelace"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}

	var signingSecret issuedSigningSecretDTO
	w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/signing-secrets", adminAuth, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("issue signing secret status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &signingSecret); err != nil {
		t.Fatalf("decode signing secret: %v", err)
	}

	now := time.Now()
	token := mintBrowserUserToken(t, signingSecret.Secret, signingSecret.ID, "HS256", user.ID, now.Unix(), now.Add(2*time.Hour).Unix())
	return orgAuth, "Bearer " + token
}

// TestFilesJourney_UploadReturnsFileIDNameMimeTypeSizeAndDownloadURL is AC1.
func TestFilesJourney_UploadReturnsFileIDNameMimeTypeSizeAndDownloadURL(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)

	const content = "upload me please"
	uploaded := uploadFile(t, wired, fixture.orgAuth, "hello.txt", "text/plain", []byte(content))

	if !strings.HasPrefix(uploaded.ID, "file_") {
		t.Errorf("id = %q, want it to start with %q", uploaded.ID, "file_")
	}
	if uploaded.Name != "hello.txt" {
		t.Errorf("name = %q, want %q", uploaded.Name, "hello.txt")
	}
	if uploaded.MimeType != "text/plain" {
		t.Errorf("mimeType = %q, want %q", uploaded.MimeType, "text/plain")
	}
	if uploaded.Size != int64(len(content)) {
		t.Errorf("size = %d, want %d", uploaded.Size, len(content))
	}
	if uploaded.DownloadURL == "" || !strings.Contains(uploaded.DownloadURL, uploaded.ID) {
		t.Errorf("downloadUrl = %q, want it to reference the file id %q", uploaded.DownloadURL, uploaded.ID)
	}
}

// TestFilesJourney_DownloadWithOrgAuthWorksAndCrossOrgIsNotFound is AC2.
func TestFilesJourney_DownloadWithOrgAuthWorksAndCrossOrgIsNotFound(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)

	const content = "download-me-please"
	uploaded := uploadFile(t, wired, fixture.orgAuth, "downloadable.txt", "text/plain", []byte(content))

	t.Run("downloading with the organization's own auth returns the stored bytes and metadata headers", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/files/"+uploaded.ID+"/download", fixture.orgAuth, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != "text/plain" {
			t.Errorf("Content-Type = %q, want %q", got, "text/plain")
		}
		if !strings.Contains(w.Header().Get("Content-Disposition"), "downloadable.txt") {
			t.Errorf("Content-Disposition = %q, want it to carry the filename", w.Header().Get("Content-Disposition"))
		}
		if got := w.Header().Get("Content-Length"); got != fmt.Sprintf("%d", len(content)) {
			t.Errorf("Content-Length = %q, want %q", got, fmt.Sprintf("%d", len(content)))
		}
		if w.Body.String() != content {
			t.Errorf("body = %q, want the uploaded content %q", w.Body.String(), content)
		}
	})

	t.Run("another organization's request for the same file id is not-found", func(t *testing.T) {
		otherOrgAuth := createSeparateOrgWithKey(t, wired, "Someone Else's Org")

		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/files/"+uploaded.ID+"/download", otherOrgAuth, "")

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if env.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
		}
	})
}

// TestFilesJourney_OversizeUploadIsRejectedAndPartialBytesAreCleanedUp is
// AC3: a tiny injected BEECON_FILE_MAX_BYTES rejects a 25-byte upload without
// the test ever allocating a real 20 MB payload, and the partial write never
// survives on disk.
func TestFilesJourney_OversizeUploadIsRejectedAndPartialBytesAreCleanedUp(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	filesDir := t.TempDir()
	const maxBytes = 10
	wired := support.BootAppWithProviderDefinitionsAndFileLimits(t, hubspotDefinitionAgainst(fakeHubspot), filesDir, maxBytes)
	fixture := newHubspotJourneyFixture(t, wired)

	oversizeContent := bytes.Repeat([]byte("x"), maxBytes+15)
	w := doMultipartUpload(t, wired.Router, fixture.orgAuth, "big.txt", "text/plain", oversizeContent)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if env.Error.Code != "validation_failed" {
		t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
	}

	entries, err := os.ReadDir(filesDir)
	if err != nil {
		t.Fatalf("read files dir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("files dir contains %v after an oversize upload was rejected, want it empty (the partial write must be cleaned up)", names)
	}
}

// TestFilesJourney_HubspotUploadFileSendsStoredFileToHubspotAndReturnsItsRecord
// is AC4: hubspot-upload-file resolves a file_ id org-scoped, streams its
// stored bytes/filename/mime to FakeHubspot's /files/v3/files as multipart,
// and returns Hubspot's own file record (not Beecon's file metadata).
func TestFilesJourney_HubspotUploadFileSendsStoredFileToHubspotAndReturnsItsRecord(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	const content = "hello from beecon file upload"
	uploaded := uploadFile(t, wired, fixture.orgAuth, "greeting.txt", "text/plain", []byte(content))

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-upload-file", fixture.userID, initiated.ID, fmt.Sprintf(`{"file":%q}`, uploaded.ID))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true; error = %+v", dto.Error)
	}
	if fakeHubspot.LastFileUpload == nil {
		t.Fatal("Hubspot never received the uploaded file")
	}
	if fakeHubspot.LastFileUpload.FieldName != "file" {
		t.Errorf("Hubspot received field name %q, want %q", fakeHubspot.LastFileUpload.FieldName, "file")
	}
	if fakeHubspot.LastFileUpload.FileName != "greeting.txt" {
		t.Errorf("Hubspot received filename %q, want %q", fakeHubspot.LastFileUpload.FileName, "greeting.txt")
	}
	if fakeHubspot.LastFileUpload.MimeType != "text/plain" {
		t.Errorf("Hubspot received Content-Type %q, want %q", fakeHubspot.LastFileUpload.MimeType, "text/plain")
	}
	if string(fakeHubspot.LastFileUpload.Content) != content {
		t.Errorf("Hubspot received content %q, want %q", string(fakeHubspot.LastFileUpload.Content), content)
	}

	data, ok := dto.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want the provider's file record object", dto.Data)
	}
	if data["id"] != "hubspot-file-1" {
		t.Errorf(`data["id"] = %v, want %q (Hubspot's own file record, not Beecon's file metadata)`, data["id"], "hubspot-file-1")
	}
}

// TestFilesJourney_UnknownOrCrossOrgFileIDReturnsEnvelopeErrorWithoutCallingTheProvider
// is AC5.
func TestFilesJourney_UnknownOrCrossOrgFileIDReturnsEnvelopeErrorWithoutCallingTheProvider(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	t.Run("an unknown file_ id is a tool-level failure and the provider is never called", func(t *testing.T) {
		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-upload-file", fixture.userID, initiated.ID, `{"file":"file_does_not_exist"}`)

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if dto.Successful {
			t.Fatal("successful = true, want false for an unknown file_ id")
		}
		if dto.Error == nil || dto.Error.Code != "file_not_found" {
			t.Fatalf("error = %+v, want code %q", dto.Error, "file_not_found")
		}
		if fakeHubspot.FilesCallCount != 0 {
			t.Errorf("Hubspot's files endpoint was called %d times, want 0", fakeHubspot.FilesCallCount)
		}
	})

	t.Run("a file_ id belonging to another organization is a tool-level failure and the provider is never called", func(t *testing.T) {
		otherOrgAuth := createSeparateOrgWithKey(t, wired, "A Different Org")
		otherFile := uploadFile(t, wired, otherOrgAuth, "not-yours.txt", "text/plain", []byte("belongs to someone else"))

		status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-upload-file", fixture.userID, initiated.ID, fmt.Sprintf(`{"file":%q}`, otherFile.ID))

		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
		if dto.Successful {
			t.Fatal("successful = true, want false for a cross-organization file_ id")
		}
		if dto.Error == nil || dto.Error.Code != "file_not_found" {
			t.Fatalf("error = %+v, want code %q", dto.Error, "file_not_found")
		}
		if fakeHubspot.FilesCallCount != 0 {
			t.Errorf("Hubspot's files endpoint was called %d times, want 0", fakeHubspot.FilesCallCount)
		}
	})
}

// TestFilesJourney_FileBytesNeverAppearInLogEntries is AC6: the log entry
// hubspot-upload-file's execution writes carries the file id and size, but
// never the raw uploaded bytes, in either its request or response body.
func TestFilesJourney_FileBytesNeverAppearInLogEntries(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	const secretContent = "THIS-EXACT-BYTE-CONTENT-MUST-NEVER-APPEAR-IN-A-LOG-ENTRY"
	uploaded := uploadFile(t, wired, fixture.orgAuth, "secret.txt", "text/plain", []byte(secretContent))

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-upload-file", fixture.userID, initiated.ID, fmt.Sprintf(`{"file":%q}`, uploaded.ID))
	if status != http.StatusOK || !dto.Successful {
		t.Fatalf("execution did not succeed: status=%d dto=%+v", status, dto)
	}

	page := fetchLogs(t, wired, fixture.orgAuth, "")
	var entry *logEntryDTO
	for i := range page.Entries {
		if page.Entries[i].ToolSlug == "hubspot-upload-file" {
			entry = &page.Entries[i]
		}
	}
	if entry == nil {
		t.Fatal("no log entry recorded for hubspot-upload-file")
	}
	if strings.Contains(entry.RequestBody, secretContent) {
		t.Fatalf("log entry's requestBody contains the raw file bytes: %s", entry.RequestBody)
	}
	if strings.Contains(entry.ResponseBody, secretContent) {
		t.Fatalf("log entry's responseBody contains the raw file bytes: %s", entry.ResponseBody)
	}
	if !strings.Contains(entry.RequestBody, uploaded.ID) {
		t.Errorf("log entry's requestBody %q does not carry the file id %q", entry.RequestBody, uploaded.ID)
	}
	if !strings.Contains(entry.RequestBody, fmt.Sprintf(`"size":%d`, len(secretContent))) {
		t.Errorf("log entry's requestBody %q does not carry the file size %d", entry.RequestBody, len(secretContent))
	}
}

// TestFilesJourney_UserTokenCannotUploadOrDownloadFiles closes the Slice 5
// deferred AC: a valid user token is rejected as unauthorized by both files
// endpoints, which are mounted org-key-only outside the OrgOrUser group.
func TestFilesJourney_UserTokenCannotUploadOrDownloadFiles(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com"})
	wired := support.BootAppWithProviderDefinitions(t, hubspotDefinitionAgainst(fakeHubspot))
	orgAuth, userAuth := newFilesUserTokenFixture(t, wired)

	uploaded := uploadFile(t, wired, orgAuth, "org-key-uploaded.txt", "text/plain", []byte("org key owns this upload"))

	t.Run("a valid user token cannot upload a file", func(t *testing.T) {
		w := doMultipartUpload(t, wired.Router, userAuth, "nope.txt", "text/plain", []byte("blocked"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	t.Run("a valid user token cannot download a file", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/files/"+uploaded.ID+"/download", userAuth, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})
}
