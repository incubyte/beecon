// pagination.go implements PD15b's tool-level pagination convention: a
// mapping's declared pagination block translates Beecon's canonical
// pageSize/cursor call arguments into a provider's own query parameter
// names, and reads the following page's cursor back out of the provider's
// decoded JSON response at a declared dotted field path (e.g. Hubspot's
// "paging.next.after") into the execution envelope's top-level nextCursor.
package execution

import (
	"fmt"
	"strings"

	"beecon/internal/catalog"
)

// canonicalPageSizeArg and canonicalCursorArg are the tool-call argument
// names every paginated tool accepts (PD15b), regardless of what the
// provider itself calls them.
const (
	canonicalPageSizeArg = "pageSize"
	canonicalCursorArg   = "cursor"
)

// applyPaginationQuery adds a tool's canonical pageSize/cursor call
// arguments, when supplied, to query under the provider's own declared
// parameter names (PD15b). A tool with no declared pagination, or a call
// that omitted a canonical argument, is left untouched for that argument.
func applyPaginationQuery(query map[string]string, pagination *catalog.Pagination, arguments map[string]any) {
	if pagination == nil {
		return
	}
	if value, ok := arguments[canonicalPageSizeArg]; ok && pagination.PageSizeParam != "" {
		query[pagination.PageSizeParam] = fmt.Sprint(value)
	}
	if value, ok := arguments[canonicalCursorArg]; ok && pagination.CursorParam != "" {
		query[pagination.CursorParam] = fmt.Sprint(value)
	}
}

// extractNextCursor reads the following page's cursor out of a decoded
// provider response at pagination's declared NextCursorPath, for the
// execution envelope's top-level nextCursor (PD15b). Returns "" when the
// tool declares no pagination, the response carries no further page, or the
// response isn't the expected shape.
func extractNextCursor(pagination *catalog.Pagination, data any) string {
	if pagination == nil || pagination.NextCursorPath == "" {
		return ""
	}
	cursor, _ := lookupNestedStringField(data, pagination.NextCursorPath)
	return cursor
}

// lookupNestedStringField walks a dotted field path (e.g. "paging.next.after")
// through decoded JSON, returning the string found there, or false when any
// segment is missing or the wrong shape.
func lookupNestedStringField(data any, path string) (string, bool) {
	current := data
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current, ok = object[segment]
		if !ok {
			return "", false
		}
	}
	value, ok := current.(string)
	return value, ok
}
