package authmw

import (
	"log/slog"
	"net/http"

	"beecon/internal/access"
)

// AttributeOperator captures the acting operator's id on mutating console
// requests (PD56, Slice 4): it reads the operator id ConsoleAuth/
// OperatorSession already injected via access.OperatorFromContext, and (for
// a POST/PUT/PATCH/DELETE only — GET/HEAD/OPTIONS are safe reads and are
// never attributed) emits a structured "operator.action" log line carrying
// operator_id, method, and path, reusing the same slog logger every other
// request-scoped log line in this codebase writes through
// (httpx.ErrorRenderer's own convention). Mount this AFTER ConsoleAuth/
// OperatorSession in the middleware chain (e.g. r.With(consoleAuth,
// AttributeOperator(logger))) — it depends on the operator id already being
// in context, not on verifying the request itself.
//
// A break-glass admin-key request injects no operator id at all
// (access.OperatorFromContext's ok is false), so there is nothing to
// attribute for those — the admin key's own demotion (AC8) means this case
// only arises pre-bootstrap anyway. logger falls back to slog.Default() when
// nil, mirroring httpx.NewErrorRenderer's own nil-safety convention.
func AttributeOperator(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isMutatingMethod(r.Method) {
				if operatorID, ok := access.OperatorFromContext(r.Context()); ok {
					logger.Info("operator.action",
						"operator_id", string(operatorID),
						"method", r.Method,
						"path", r.URL.Path,
					)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
