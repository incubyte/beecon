// Package providerhttp_test exercises Client.Call against a real
// httptest.Server: the one behavioral gap facade_test.go's fakes cannot
// close, since ProviderClient is faked there — this proves ToolCallRequest's
// declared header mapping (PD13) actually lands on the wire as an HTTP
// header, not just inside an in-memory struct field.
package providerhttp_test

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"beecon/internal/execution"
	"beecon/internal/execution/driven/providerhttp"
)

func TestCall_ForwardsDeclaredHeadersOnTheActualHTTPRequest(t *testing.T) {
	var receivedPrefer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPrefer = r.Header.Get("Prefer")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{
		Method:      http.MethodGet,
		URL:         server.URL,
		AccessToken: "token-value",
		Headers:     map[string]string{"Prefer": "return=minimal"},
	}

	_, err := client.Call(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedPrefer != "return=minimal" {
		t.Errorf("received Prefer header = %q, want %q", receivedPrefer, "return=minimal")
	}
}

func TestCall_SendsNoExtraHeaderWhenNoneAreDeclared(t *testing.T) {
	var receivedPrefer string
	sawHeader := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPrefer, sawHeader = r.Header.Get("Prefer"), r.Header.Get("Prefer") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{Method: http.MethodGet, URL: server.URL, AccessToken: "token-value"}

	_, err := client.Call(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sawHeader {
		t.Errorf("received Prefer header = %q, want no Prefer header sent at all", receivedPrefer)
	}
}

// --- PD22 (Slice 7): multipart body construction for file-typed inputs ---

// TestCall_SendsAResolvedFileAsMultipartFormData proves buildMultipartBody
// actually lands the resolved file-typed input on the wire as a real
// multipart/form-data part — field name, filename, content type, and bytes —
// not just inside an in-memory ToolCallFile.
func TestCall_SendsAResolvedFileAsMultipartFormData(t *testing.T) {
	const content = "the exact bytes a file-typed tool call must carry"
	var receivedContentType string
	var receivedFieldName, receivedFileName, receivedFileContentType string
	var receivedContent []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("server: parse multipart form: %v", err)
		}
		if len(r.MultipartForm.File) != 1 {
			t.Fatalf("server: received files under %d field names, want exactly 1", len(r.MultipartForm.File))
		}
		var header *multipart.FileHeader
		for fieldName, files := range r.MultipartForm.File {
			if len(files) != 1 {
				t.Fatalf("server: received %d files under field %q, want 1", len(files), fieldName)
			}
			receivedFieldName = fieldName
			header = files[0]
		}
		receivedFileName = header.Filename
		receivedFileContentType = header.Header.Get("Content-Type")
		f, err := header.Open()
		if err != nil {
			t.Fatalf("server: open uploaded file part: %v", err)
		}
		defer f.Close()
		receivedContent, _ = io.ReadAll(f)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"provider-file-1"}`))
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{
		Method:      http.MethodPost,
		URL:         server.URL,
		AccessToken: "token-value",
		Files: []execution.ToolCallFile{
			{FieldName: "file", FileID: "file_abc", FileName: "report.pdf", MimeType: "application/pdf", Size: int64(len(content)), Content: []byte(content)},
		},
	}

	resp, err := client.Call(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	mediaType, _, err := mime.ParseMediaType(receivedContentType)
	if err != nil {
		t.Fatalf("parse received Content-Type %q: %v", receivedContentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("Content-Type media type = %q, want %q", mediaType, "multipart/form-data")
	}
	if receivedFieldName != "file" {
		t.Errorf("received field name = %q, want %q", receivedFieldName, "file")
	}
	if receivedFileName != "report.pdf" {
		t.Errorf("received filename = %q, want %q", receivedFileName, "report.pdf")
	}
	if receivedFileContentType != "application/pdf" {
		t.Errorf("received part Content-Type = %q, want %q", receivedFileContentType, "application/pdf")
	}
	if string(receivedContent) != content {
		t.Errorf("received content = %q, want %q", string(receivedContent), content)
	}
}

// TestCall_MultipartTakesPriorityOverAJSONBodyWhenBothArePresent locks in
// buildRequestBody's declared priority (its own doc comment): a request
// carrying resolved files always sends multipart, never the JSON body, even
// if both happen to be set.
func TestCall_MultipartTakesPriorityOverAJSONBodyWhenBothArePresent(t *testing.T) {
	var receivedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{
		Method:      http.MethodPost,
		URL:         server.URL,
		AccessToken: "token-value",
		Body:        `{"should":"be ignored"}`,
		Files: []execution.ToolCallFile{
			{FieldName: "file", FileName: "a.txt", Content: []byte("x")},
		},
	}

	if _, err := client.Call(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mediaType, _, err := mime.ParseMediaType(receivedContentType)
	if err != nil {
		t.Fatalf("parse received Content-Type %q: %v", receivedContentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("Content-Type media type = %q, want %q (files take priority over a JSON body)", mediaType, "multipart/form-data")
	}
}

// TestCall_SendsNoBodyOrContentTypeWhenNeitherFilesNorBodyAreSet proves the
// GET-tool fallback (Phase 1's shape) still sends no body at all.
func TestCall_SendsNoBodyOrContentTypeWhenNeitherFilesNorBodyAreSet(t *testing.T) {
	var receivedContentType string
	var receivedContentLength int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedContentLength = r.ContentLength
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := providerhttp.NewClient(nil)
	req := execution.ToolCallRequest{Method: http.MethodGet, URL: server.URL, AccessToken: "token-value"}

	if _, err := client.Call(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "" {
		t.Errorf("received Content-Type = %q, want none sent", receivedContentType)
	}
	if receivedContentLength > 0 {
		t.Errorf("received Content-Length = %d, want no body sent", receivedContentLength)
	}
}
