package httpapi

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"beecon/internal/execution"
	"beecon/internal/httpx"
	"beecon/internal/organizations"
)

// FilesHandler serves Slice 7's FileUpload endpoints (PD22): POST
// /api/v1/files (multipart upload) and GET /api/v1/files/{fileId}/download.
// Both are org-key-only — mounted outside the OrgOrUser group in
// app/router.go, never reachable by a user token (Slice 5's deferred AC).
// It is a separate type from Handler (not additional methods on it) because
// it needs baseURL to build a downloadUrl, which Handler's own route never
// does.
type FilesHandler struct {
	facade  *execution.Facade
	errors  *httpx.ErrorRenderer
	baseURL string
}

func NewFilesHandler(facade *execution.Facade, errors *httpx.ErrorRenderer, baseURL string) *FilesHandler {
	return &FilesHandler{facade: facade, errors: errors, baseURL: baseURL}
}

// Upload handles POST /api/v1/files (AC1, AC3): a multipart request with one
// file part, streamed straight into UploadFile without ever buffering the
// whole file in the handler.
func (h *FilesHandler) Upload(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	part, err := firstFilePart(r)
	if err != nil {
		h.errors.WriteError(w, r, execution.ErrValidation("file", "request must be multipart/form-data with a file part"))
		return
	}
	defer part.Close()

	uploaded, err := h.facade.UploadFile(r.Context(), org, part.FileName(), partContentType(part), part)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toUploadedFileDTO(uploaded, h.downloadURL(uploaded.ID)))
}

// Download handles GET /api/v1/files/{fileId}/download (AC2): org-scoped —
// an unknown or cross-organization id is not-found, never a leak of another
// organization's file existing.
func (h *FilesHandler) Download(w http.ResponseWriter, r *http.Request) {
	org, ok := organizations.OrgIDFromContext(r.Context())
	if !ok {
		h.errors.WriteError(w, r, httpx.Unauthorized("missing organization context"))
		return
	}
	fileID := execution.FileID(chi.URLParam(r, "fileId"))

	metadata, content, err := h.facade.DownloadFile(r.Context(), org, fileID)
	if err != nil {
		h.errors.WriteError(w, r, err)
		return
	}
	defer content.Close()

	w.Header().Set("Content-Type", metadata.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", metadata.Name))
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.Size, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

func (h *FilesHandler) downloadURL(id execution.FileID) string {
	return strings.TrimRight(h.baseURL, "/") + "/api/v1/files/" + string(id) + "/download"
}

// firstFilePart returns the request's first multipart part that carries a
// filename (a file field, not a plain form value), streamed directly from
// the request body via multipart.Reader — the request is never fully
// buffered.
func firstFilePart(r *http.Request) (*multipart.Part, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FileName() != "" {
			return part, nil
		}
		_ = part.Close()
	}
}

func partContentType(part *multipart.Part) string {
	if contentType := part.Header.Get("Content-Type"); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
