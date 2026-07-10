//go:build integration

// Package crucial_path holds end-to-end integration tests that boot the full
// app (real app.Wire, real chi router, real SQLite-backed persistence adapter)
// and drive it through httptest, story-style. Wire shapes are re-declared here
// rather than imported so the tests pin the actual JSON contract independent
// of the feature packages.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"beecon/test/support"
)

type organizationDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

type wireErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func doJSONRequest(t *testing.T, handler http.Handler, method, path, authHeader, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// TestOrganizationsJourney tells the Slice 1 story end to end against the
// real composition root: boot -> health -> create-without-key rejected ->
// create-with-key succeeds -> fetch it -> unknown id not-found -> empty name
// rejected.
func TestOrganizationsJourney(t *testing.T) {
	wired := support.BootApp(t)

	t.Run("health reports ok once booted against the real database", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/health", "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("health status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("creating an organization without the admin key is unauthorized", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", "", `{"name":"Acme"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
		}
	})

	authHeader := "Bearer " + support.AdminAPIKey
	var created organizationDTO

	t.Run("creating an organization with the admin key succeeds", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", authHeader, `{"name":"Acme"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create response: %v; body=%s", err, w.Body.String())
		}
		if !strings.HasPrefix(created.ID, "org_") {
			t.Errorf("id = %q, want it to start with %q", created.ID, "org_")
		}
		if created.Name != "Acme" {
			t.Errorf("name = %q, want %q", created.Name, "Acme")
		}
	})

	t.Run("fetching the created organization by id succeeds", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/"+created.ID, authHeader, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
		var got organizationDTO
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode get response: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("id = %q, want %q", got.ID, created.ID)
		}
	})

	t.Run("fetching an unknown organization id returns not-found", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/api/v1/organizations/org_does_not_exist", authHeader, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if env.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "not_found")
		}
	})

	t.Run("creating an organization with an empty name is a validation error", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", authHeader, `{"name":""}`)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
		}
		var env wireErrorEnvelope
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if env.Error.Code != "validation_failed" {
			t.Errorf("error.code = %q, want %q", env.Error.Code, "validation_failed")
		}
	})
}

// TestOrganizationsSurviveRestartAndMigrationsAreIdempotent boots the app
// twice against the same SQLite database (simulating a process restart): the
// organization created before the "restart" is still fetchable afterward, and
// the second boot's migration run does not fail or duplicate schema objects.
func TestOrganizationsSurviveRestartAndMigrationsAreIdempotent(t *testing.T) {
	dsn := support.NewTestDSN(t)
	authHeader := "Bearer " + support.AdminAPIKey

	firstBoot := support.BootAppAt(t, dsn)
	w := doJSONRequest(t, firstBoot.Router, http.MethodPost, "/api/v1/organizations/", authHeader, `{"name":"Acme"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("first-boot create status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	var created organizationDTO
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// Re-wire the app against the same DSN without closing the first
	// connection, exercising the boot migrator's idempotency (Migrate must
	// not fail or re-apply an already-applied migration) the same way a
	// process restart against a persistent database would.
	secondBoot := support.BootAppAt(t, dsn)

	getAfterRestart := doJSONRequest(t, secondBoot.Router, http.MethodGet, "/api/v1/organizations/"+created.ID, authHeader, "")
	if getAfterRestart.Code != http.StatusOK {
		t.Fatalf("post-restart get status = %d, want %d; body=%s", getAfterRestart.Code, http.StatusOK, getAfterRestart.Body.String())
	}
	var got organizationDTO
	if err := json.Unmarshal(getAfterRestart.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id after restart = %q, want %q (data must survive the restart)", got.ID, created.ID)
	}
	if got.Name != "Acme" {
		t.Errorf("name after restart = %q, want %q", got.Name, "Acme")
	}
}
