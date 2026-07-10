package httpx

import (
	"encoding/json"
	"net/http"
)

// DecodeJSON decodes the request body into dst, treating an empty body as a
// zero-value struct so handlers can rely on validation rather than decode
// errors for missing optional fields.
func DecodeJSON(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(dst)
}
