// Package authmw_test (see console_auth_test.go's own header for the shared
// conventions reused here). This file covers Phase 5 Slice 4's
// AttributeOperator middleware (PD56, AC7): a mutating console request
// attributed to the operator context.WithOperator already injected emits an
// "operator.action" log line carrying operator_id/method/path; a safe read
// (GET) never does, and neither does a request with no operator in context
// at all (the pre-bootstrap admin-key branch).
package authmw_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/internal/access"
	"beecon/internal/access/driving/authmw"
)

const attributeTestOperatorID = access.OperatorID("op_attributed")

// newAttributeOperatorHandler wires AttributeOperator(logger) in front of a
// terminal no-op handler, mirroring newConsoleAuthHandler's own "wrap a
// no-op next" convention — this file tests AttributeOperator in isolation,
// not the ConsoleAuth/OperatorSession chain it is meant to sit behind.
func newAttributeOperatorHandler(logger *slog.Logger) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return authmw.AttributeOperator(logger)(next)
}

func doAttributeOperatorRequest(h http.Handler, method string, operatorID access.OperatorID, hasOperator bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/operators", nil)
	if hasOperator {
		ctx := access.WithOperator(req.Context(), operatorID)
		req = req.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAttributeOperator_LogsTheOperatorIDMethodAndPathOnAMutatingRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := newAttributeOperatorHandler(logger)

	w := doAttributeOperatorRequest(h, http.MethodPost, attributeTestOperatorID, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	logged := buf.String()
	if !containsAll(logged, "operator.action", `"operator_id":"op_attributed"`, `"method":"POST"`, `"path":"/api/v1/operators"`) {
		t.Fatalf("log output = %q, want an operator.action line carrying operator_id/method/path", logged)
	}
}

func TestAttributeOperator_NeverLogsForASafeGETRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := newAttributeOperatorHandler(logger)

	w := doAttributeOperatorRequest(h, http.MethodGet, attributeTestOperatorID, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for a GET request, got: %s", buf.String())
	}
}

func TestAttributeOperator_NeverLogsWhenNoOperatorIsInContext(t *testing.T) {
	// The pre-bootstrap admin-key branch of ConsoleAuth injects no operator
	// id at all — there is nothing to attribute, so a mutating request that
	// somehow reaches AttributeOperator without one must not log a line with
	// an empty/garbage operator_id.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := newAttributeOperatorHandler(logger)

	w := doAttributeOperatorRequest(h, http.MethodPost, "", false)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output when no operator is in context, got: %s", buf.String())
	}
}

func TestAttributeOperator_LogsForEveryMutatingMethodNotJustPost(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			h := newAttributeOperatorHandler(logger)

			doAttributeOperatorRequest(h, method, attributeTestOperatorID, true)

			if !containsAll(buf.String(), "operator.action", `"method":"`+method+`"`) {
				t.Errorf("method %s: log output = %q, want an operator.action line", method, buf.String())
			}
		})
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
