// Package httpapi (in-package test) exercises FilesHandler.Upload's own
// request-shape validation (PD22, Slice 7) through an actual chi router,
// reusing newTestRouter's org-context helper (doRequestAsOrg) from
// handler_test.go, same package: a request that is not multipart/form-data
// at all, and a multipart request that carries no file part, must both be
// rejected as a 422 validation_failed before ever reaching UploadFile.
package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"beecon/internal/execution"
	"beecon/internal/execution/driven/memory"
	"beecon/internal/httpx"
	"beecon/internal/idgen"
	"beecon/internal/organizations"
)

func newTestFilesRouter() http.Handler {
	facade := execution.NewFacade(fakeToolReader{}, fakeConnectionReader{}, fakeProviderClient{}, nil, func() time.Time { return time.Now() }).
		WithFiles(memory.NewFilesRepository(), memory.NewFileStore(), 1024, idgen.Prefixed("file_"))
	errorRenderer := httpx.NewErrorRenderer(nil)
	h := NewFilesHandler(facade, errorRenderer, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", h.Upload)
	return mux
}

func doUploadRequest(handler http.Handler, org organizations.OrgID, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if org != "" {
		req = req.WithContext(organizations.WithOrgID(req.Context(), org))
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// TestUpload_Returns422WhenTheRequestBodyIsNotMultipart covers
// files_handler.go's firstFilePart error branch for the simplest case: a
// plain JSON body carries no multipart boundary at all.
func TestUpload_Returns422WhenTheRequestBodyIsNotMultipart(t *testing.T) {
	router := newTestFilesRouter()

	w := doUploadRequest(router, testOrg, "application/json", []byte(`{"not":"multipart"}`))

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	assertValidationFailedEnvelope(t, w.Body.Bytes())
}

// TestUpload_Returns422WhenAMultipartRequestCarriesNoFilePart covers
// firstFilePart's other error branch: the request is genuinely
// multipart/form-data, but every part is a plain form field (no filename),
// so NextPart eventually returns io.EOF without ever finding a file part.
func TestUpload_Returns422WhenAMultipartRequestCarriesNoFilePart(t *testing.T) {
	router := newTestFilesRouter()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("notAFile", "just a plain form value"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	w := doUploadRequest(router, testOrg, writer.FormDataContentType(), body.Bytes())

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
	assertValidationFailedEnvelope(t, w.Body.Bytes())
}

func assertValidationFailedEnvelope(t *testing.T, body []byte) {
	t.Helper()
	if !bytes.Contains(body, []byte(`"validation_failed"`)) {
		t.Errorf("body %s does not carry the validation_failed error code", body)
	}
}
