// Package providerhttp is the execution module's real driven ProviderClient:
// one bearer-authenticated HTTP call per tool execution (stdlib net/http —
// Beecon's tool call is a single request, not a general API client).
package providerhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"beecon/internal/execution"
)

// defaultTimeout bounds every request this client makes to a provider — a
// tool execution should never hang the caller indefinitely.
const defaultTimeout = 15 * time.Second

// Client is the execution module's real driven ProviderClient.
type Client struct {
	httpClient *http.Client
}

var _ execution.ProviderClient = (*Client)(nil)

// NewClient builds a Client. A nil httpClient falls back to one with
// defaultTimeout.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

// Call makes one bearer-authenticated HTTP request to a provider's tool
// endpoint (PD8: GET /v1.0/me/messages on Microsoft Graph, with
// top/skip/select/filter carried as query parameters; PD13: a POST tool with
// a declared body mapping, e.g. Hubspot's create-contact, carries req.Body as
// a JSON request body; PD22: a POST tool with resolved file-typed inputs,
// e.g. hubspot-upload-file, carries req.Files as a multipart request
// instead). It returns an error only when the provider could not be reached
// at all; a non-2xx response is returned as a normal ToolCallResponse so
// Execute can turn it into AC7's tool-level failure.
func (c *Client) Call(ctx context.Context, req execution.ToolCallRequest) (execution.ToolCallResponse, error) {
	requestURL, err := buildRequestURL(req.URL, req.Query)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("build tool call url: %w", err)
	}

	requestBody, contentType, err := buildRequestBody(req)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("build tool call body: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, requestURL, requestBody)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("build tool call request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.AccessToken)
	httpReq.Header.Set("Accept", "application/json")
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("call provider: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return execution.ToolCallResponse{}, fmt.Errorf("read provider response: %w", err)
	}
	return execution.ToolCallResponse{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		RetryAfter: resp.Header.Get("Retry-After"),
	}, nil
}

// buildRequestURL appends query onto rawURL's own query string (rawURL is
// the tool's full call URL from its provider definition).
func buildRequestURL(rawURL string, query map[string]string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

// buildRequestBody picks req's request body and Content-Type (PD22): a
// multipart body when req carries resolved file-typed inputs (takes priority
// over a JSON body — no Beecon tool declares both), otherwise req.Body as
// JSON when non-empty, otherwise no body at all (every GET tool).
func buildRequestBody(req execution.ToolCallRequest) (io.Reader, string, error) {
	if len(req.Files) > 0 {
		return buildMultipartBody(req.Files)
	}
	if req.Body != "" {
		return strings.NewReader(req.Body), "application/json", nil
	}
	return nil, "", nil
}

// buildMultipartBody renders files as a multipart/form-data body (PD22): one
// part per resolved file-typed input, named by its FieldName. Files are
// already capped at the execution facade's configured maximum size, so
// building the whole body in memory here is a bounded, acceptable cost.
func buildMultipartBody(files []execution.ToolCallFile) (io.Reader, string, error) {
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	for _, file := range files {
		if err := writeMultipartFile(writer, file); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer, writer.FormDataContentType(), nil
}

func writeMultipartFile(writer *multipart.Writer, file execution.ToolCallFile) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, file.FieldName, file.FileName))
	if file.MimeType != "" {
		header.Set("Content-Type", file.MimeType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(file.Content)
	return err
}
