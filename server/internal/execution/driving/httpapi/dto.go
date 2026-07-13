package httpapi

import "beecon/internal/execution"

// executeToolRequest is the POST /api/v1/tools/{slug}/execute body.
type executeToolRequest struct {
	UserID       string         `json:"userId"`
	ConnectionID string         `json:"connectionId"`
	Arguments    map[string]any `json:"arguments"`
}

// executionErrorDTO is the tool-level error carried inside a non-successful
// result (PD6).
type executionErrorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// executionResultDTO is Execute's response: {successful, error, data,
// nextCursor} (PD6, AC1; nextCursor added PD15b) — always HTTP 200 for a
// tool-level outcome, whichever it is.
type executionResultDTO struct {
	Successful bool               `json:"successful"`
	Error      *executionErrorDTO `json:"error"`
	Data       any                `json:"data"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

func toExecutionResultDTO(result execution.Result) executionResultDTO {
	dto := executionResultDTO{Successful: result.Successful, Data: result.Data, NextCursor: result.NextCursor}
	if result.Error != nil {
		dto.Error = &executionErrorDTO{Code: result.Error.Code, Message: result.Error.Message}
	}
	return dto
}

// uploadedFileDTO is the response to FilesHandler.Upload (PD22, AC1):
// {id, name, mimeType, size, downloadUrl}.
type uploadedFileDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MimeType    string `json:"mimeType"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"downloadUrl"`
}

func toUploadedFileDTO(uploaded execution.UploadedFile, downloadURL string) uploadedFileDTO {
	return uploadedFileDTO{
		ID:          string(uploaded.ID),
		Name:        uploaded.Name,
		MimeType:    uploaded.MimeType,
		Size:        uploaded.Size,
		DownloadURL: downloadURL,
	}
}
