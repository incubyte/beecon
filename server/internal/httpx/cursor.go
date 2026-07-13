package httpx

import (
	"encoding/base64"
	"errors"
	"strings"
)

// cursorFieldSeparator joins EncodeCursor's fields inside the opaque,
// base64-encoded pagination cursor every list endpoint returns (PD10/PD15b's
// platform-wide convention): cursor()s themselves are strings tools/logging/
// connections chose to be free of this separator (slugs, RFC3339Nano
// timestamps, CUID2 ids).
const cursorFieldSeparator = "|"

// ErrMalformedCursor is DecodeCursor's error for a cursor that isn't valid
// base64, or doesn't decode into exactly the field count the caller expects.
// Every module wraps this into its own PD5 validation_failed DomainError
// (catalog.ErrInvalidCursor, logging.ErrInvalidCursor, and so on) rather than
// exposing this directly to a consumer.
var ErrMalformedCursor = errors.New("malformed pagination cursor")

// EncodeCursor joins fields with cursorFieldSeparator and base64-encodes the
// result, producing the opaque cursor a list endpoint returns as
// nextCursor — extracted from logging.Query's and catalog.ListTools' own,
// previously duplicated, cursor encoding (third occurrence, Slice 4's
// tidy-first).
func EncodeCursor(fields ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, cursorFieldSeparator)))
}

// DecodeCursor reverses EncodeCursor, splitting the decoded string into
// exactly fieldCount fields. An empty raw is the "no cursor" case (the first
// page): it returns a nil slice and a nil error, distinct from a malformed
// one. Any other failure — invalid base64, the wrong number of fields, or an
// empty field — returns ErrMalformedCursor.
func DecodeCursor(raw string, fieldCount int) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrMalformedCursor
	}
	fields := strings.Split(string(decoded), cursorFieldSeparator)
	if len(fields) != fieldCount {
		return nil, ErrMalformedCursor
	}
	for _, field := range fields {
		if field == "" {
			return nil, ErrMalformedCursor
		}
	}
	return fields, nil
}
