//go:build integration

// Package crucial_path (see organizations_journey_integration_test.go for the
// package header; reuses organizationDTO, issuedKeyDTO, userDTO, wireErrorEnvelope,
// doJSONRequest, oauthJourneyFixture/newOAuthJourneyFixture,
// openConnectPageAndGetState, outlookDefinitionWithFakeGraphTool,
// activateConnectionThroughRealHandshake, executeTool, executionResultDTO,
// fetchLogs, logsPageDTO from tool_execution_journey_integration_test.go;
// hubspotDefinitionAgainst, newHubspotJourneyFixture,
// activateHubspotConnection, executeHubspotTool from
// hubspot_journey_integration_test.go — same package). This file tells
// Slice 6's story end to end against the real composition root: Graph's and
// Hubspot's distinctly shaped 429 responses both retry platform-side,
// honoring Retry-After (or falling back to a jittered backoff) via an
// injected SleepSpy so the waits are asserted on without a real delay;
// a retry that succeeds leaks no rate-limit detail; exhaustion surfaces as
// HTTP 429 with a Retry-After header and code "rate_limited"; a non-retriable
// upstream error is never retried; every attempt — rate-limited or not —
// writes its own log entry; and the admin-guarded /metrics endpoint exposes
// execution counts/durations, rate-limit retries, OAuth handshake outcomes,
// and token-refresh outcomes.
package crucial_path

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"beecon/internal/app"
	"beecon/test/support"
)

func scrapeMetrics(t *testing.T, wired *app.Wired) (int, string) {
	t.Helper()
	w := doJSONRequest(t, wired.Router, http.MethodGet, "/metrics", "Bearer "+support.AdminAPIKey, "")
	return w.Code, w.Body.String()
}

// TestRateLimitJourney_GraphRateLimitRetriedThenSucceedsHonoringRetryAfterWithNoLeakedDetail
// is AC1 and AC2: Graph's normalized 429 (nested error.innerError.code) is
// retried once, honoring the Retry-After header the fake sent, and the
// consumer sees a normal successful envelope with no rate-limit detail
// leaked anywhere in it.
func TestRateLimitJourney_GraphRateLimitRetriedThenSucceedsHonoringRetryAfterWithNoLeakedDetail(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{RateLimitedAttempts: 1, RateLimitRetryAfter: "3"})
	sleepSpy := &support.SleepSpy{}
	wired := support.BootAppWithProviderDefinitionsAndSleep(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), sleepSpy.Sleep)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true once the retried call succeeds; error = %+v", dto.Error)
	}
	if dto.Error != nil {
		t.Errorf("error = %+v, want nil", dto.Error)
	}
	if fakeGraph.MessagesCallCount != 2 {
		t.Errorf("Graph received %d calls, want exactly 2 (one rate-limited attempt, one retried success)", fakeGraph.MessagesCallCount)
	}
	if durations := sleepSpy.Durations(); len(durations) != 1 || durations[0] != 3*time.Second {
		t.Errorf("sleep durations = %v, want exactly [3s] — the retry loop must honor Graph's own Retry-After", durations)
	}

	t.Run("the rate-limited attempt and its successful retry each wrote their own marked log entry", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&toolSlug=outlook-list-messages&limit=100")
		if len(page.Entries) != 2 {
			t.Fatalf("got %d tool_execution entries, want exactly 2 (one per attempt)", len(page.Entries))
		}
		var sawRateLimited, sawSuccess bool
		for _, entry := range page.Entries {
			switch entry.Status {
			case http.StatusTooManyRequests:
				sawRateLimited = true
				if !entry.RateLimited {
					t.Errorf("429 entry %+v has rateLimited = false, want true", entry)
				}
			case http.StatusOK:
				sawSuccess = true
				if entry.RateLimited {
					t.Errorf("200 entry %+v has rateLimited = true, want false", entry)
				}
			}
		}
		if !sawRateLimited {
			t.Errorf("no logged entry carries status 429 among %+v", page.Entries)
		}
		if !sawSuccess {
			t.Errorf("no logged entry carries status 200 among %+v", page.Entries)
		}
	})
}

// TestRateLimitJourney_GraphRateLimitExhaustedReturnsHTTP429WithRetryAfterHeaderAndRateLimitedCode
// is AC3: every attempt against a normalized rate limit stays rate-limited,
// so the execute endpoint responds as the PD21/ADR-0009 carve-out — HTTP 429
// with a Retry-After header and code "rate_limited" — not the usual PD6
// envelope.
func TestRateLimitJourney_GraphRateLimitExhaustedReturnsHTTP429WithRetryAfterHeaderAndRateLimitedCode(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{RateLimitedAttempts: 10, RateLimitRetryAfter: "2"})
	sleepSpy := &support.SleepSpy{}
	wired := support.BootAppWithProviderDefinitionsAndSleep(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), sleepSpy.Sleep)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	body := `{"userId":"` + fixture.userID + `","connectionId":"` + initiated.ID + `","arguments":{}}`
	w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/tools/outlook-list-messages/execute", fixture.orgAuth, body)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusTooManyRequests, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != "2" {
		t.Errorf(`Retry-After header = %q, want %q`, got, "2")
	}
	var envelope wireErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, w.Body.String())
	}
	if envelope.Error.Code != "rate_limited" {
		t.Errorf("error.code = %q, want %q", envelope.Error.Code, "rate_limited")
	}
	if fakeGraph.MessagesCallCount != 3 {
		t.Errorf("Graph received %d calls, want exactly 3 (PD21's retry ceiling)", fakeGraph.MessagesCallCount)
	}

	t.Run("every exhausted attempt still wrote its own log entry marked rate-limited", func(t *testing.T) {
		page := fetchLogs(t, wired, fixture.orgAuth, "?connectionId="+initiated.ID+"&toolSlug=outlook-list-messages&limit=100")
		if len(page.Entries) != 3 {
			t.Fatalf("got %d tool_execution entries, want exactly 3 (one per exhausted attempt)", len(page.Entries))
		}
		for _, entry := range page.Entries {
			if entry.Status != http.StatusTooManyRequests {
				t.Errorf("entry status = %d, want %d for every exhausted attempt", entry.Status, http.StatusTooManyRequests)
			}
			if !entry.RateLimited {
				t.Errorf("entry %+v has rateLimited = false, want true for every exhausted attempt", entry)
			}
		}
	})
}

// TestRateLimitJourney_HubspotRateLimitsCategoryRetriedThenSucceedsWithJitteredBackoff
// is AC1's Hubspot half: Hubspot's RATE_LIMITS-category 429 (no Retry-After
// header) is retried too, falling back to PD21's jittered backoff.
func TestRateLimitJourney_HubspotRateLimitsCategoryRetriedThenSucceedsWithJitteredBackoff(t *testing.T) {
	fakeHubspot := support.NewFakeHubspot(t, support.FakeHubspotScript{
		AccessToken: "at", RefreshToken: "rt", AccountEmail: "ada@example.com", HubDomain: "acme.hubspot.com",
		RateLimitedAttempts: 1,
		Contacts:            []support.FakeHubspotContact{{ID: "contact-1", Properties: map[string]string{"email": "one@example.com"}}},
	})
	sleepSpy := &support.SleepSpy{}
	wired := support.BootAppWithProviderDefinitionsAndSleep(t, hubspotDefinitionAgainst(fakeHubspot), sleepSpy.Sleep)
	fixture := newHubspotJourneyFixture(t, wired)
	initiated := activateHubspotConnection(t, wired, fixture)

	status, dto := executeHubspotTool(t, wired, fixture.orgAuth, "hubspot-list-contacts", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	if !dto.Successful {
		t.Fatalf("successful = false, want true once the retried call succeeds; error = %+v", dto.Error)
	}
	if fakeHubspot.ContactsCallCount != 2 {
		t.Errorf("Hubspot received %d calls, want exactly 2 (one rate-limited attempt, one retried success)", fakeHubspot.ContactsCallCount)
	}
	// PD21's jittered-backoff bounds (retry.go: jitterBackoffMin/Max) — no
	// Retry-After header means the loop must fall back to a randomized wait
	// within this documented range rather than not waiting at all.
	const jitterMin, jitterMax = 500 * time.Millisecond, 2 * time.Second
	durations := sleepSpy.Durations()
	if len(durations) != 1 {
		t.Fatalf("sleep durations = %v, want exactly one jittered backoff", durations)
	}
	if durations[0] < jitterMin || durations[0] > jitterMax {
		t.Errorf("backoff = %v, want within [%v, %v]", durations[0], jitterMin, jitterMax)
	}
}

// TestRateLimitJourney_NonRetriableUpstreamErrorsAreNotRetried is AC4: a
// plain upstream 404 is never mistaken for a rate limit and surfaces once,
// as the usual tool-level failure.
func TestRateLimitJourney_NonRetriableUpstreamErrorsAreNotRetried(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{AccessToken: rawGraphAccessToken, RefreshToken: "rt", AccountEmail: "ada@example.com", AccountDisplayName: "Ada"})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{StatusCode: http.StatusNotFound, Body: `{"error":"ItemNotFound"}`})
	sleepSpy := &support.SleepSpy{}
	wired := support.BootAppWithProviderDefinitionsAndSleep(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), sleepSpy.Sleep)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture)

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d (a non-retriable upstream error is a tool-level failure)", status, http.StatusOK)
	}
	if dto.Successful {
		t.Error("successful = true, want false for a 404 upstream response")
	}
	if dto.Error == nil || dto.Error.Code != "provider_error" {
		t.Fatalf("error = %+v, want code %q", dto.Error, "provider_error")
	}
	if fakeGraph.MessagesCallCount != 1 {
		t.Errorf("Graph received %d calls, want exactly 1 — a non-retriable status must never be retried", fakeGraph.MessagesCallCount)
	}
	if len(sleepSpy.Durations()) != 0 {
		t.Errorf("slept %d times, want 0 — a non-retriable status must never wait", len(sleepSpy.Durations()))
	}
}

// TestRateLimitMetricsJourney_MetricsEndpointExposesExecutionRetryHandshakeAndRefreshOutcomes
// is AC6's content half: a rate-limited-then-successful execution, the OAuth
// handshake the connect flow already ran, and a clock-travel-forced token
// refresh together exercise every PD24 metric family, then the admin-guarded
// scrape must expose them all.
func TestRateLimitMetricsJourney_MetricsEndpointExposesExecutionRetryHandshakeAndRefreshOutcomes(t *testing.T) {
	fakeMS := support.NewFakeMicrosoft(t, support.FakeMicrosoftScript{
		AccessToken: "original-access-token", RefreshToken: "original-refresh-token",
		AccountEmail: "ada@example.com", AccountDisplayName: "Ada Lovelace",
		ExpiresIn:           60,
		RefreshAccessToken:  "refreshed-access-token",
		RefreshRefreshToken: "rotated-refresh-token",
	})
	fakeGraph := support.NewFakeGraph(t, support.FakeGraphScript{RateLimitedAttempts: 1, RateLimitRetryAfter: "1"})
	clock := support.NewMovableClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	sleepSpy := &support.SleepSpy{}
	wired := support.BootAppWithProviderDefinitionsClockAndSleep(t, outlookDefinitionWithFakeGraphTool(fakeMS, fakeGraph), clock.Now, sleepSpy.Sleep)
	fixture := newOAuthJourneyFixture(t, wired)
	initiated := activateConnectionThroughRealHandshake(t, wired, fixture) // OAuth handshake outcome metric

	status, dto := executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`) // execution + rate-limit-retry metrics
	if status != http.StatusOK || !dto.Successful {
		t.Fatalf("rate-limited-then-successful execution failed: status=%d dto=%+v", status, dto)
	}

	clock.Advance(2 * time.Minute)                                                                                    // past the scripted 60s ExpiresIn
	status, dto = executeTool(t, wired, fixture.orgAuth, "outlook-list-messages", fixture.userID, initiated.ID, `{}`) // token-refresh outcome metric
	if status != http.StatusOK || !dto.Successful {
		t.Fatalf("post-expiry execution failed: status=%d dto=%+v", status, dto)
	}
	if fakeMS.RefreshCallCount != 1 {
		t.Fatalf("RefreshCallCount = %d, want exactly 1", fakeMS.RefreshCallCount)
	}

	statusCode, body := scrapeMetrics(t, wired)
	if statusCode != http.StatusOK {
		t.Fatalf("metrics scrape status = %d, want %d; body=%s", statusCode, http.StatusOK, body)
	}
	for _, want := range []string{
		"beecon_tool_executions_total",
		"beecon_tool_execution_duration_seconds",
		"beecon_rate_limit_retries_total",
		"beecon_oauth_handshakes_total",
		"beecon_token_refreshes_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics scrape does not contain %q:\n%s", want, body)
		}
	}
	for _, wantLabel := range []string{
		`provider="outlook"`,
		`outcome="success"`,
		`status="200"`,
	} {
		if !strings.Contains(body, wantLabel) {
			t.Errorf("metrics scrape does not carry label %s:\n%s", wantLabel, body)
		}
	}
}

// TestRateLimitMetricsJourney_MetricsEndpointIsAdminGuarded is AC6's guard
// half: /metrics is never reachable with no key or with an org's own API
// key — only the installation admin key works.
func TestRateLimitMetricsJourney_MetricsEndpointIsAdminGuarded(t *testing.T) {
	wired := support.BootApp(t)

	t.Run("no Authorization header is rejected", func(t *testing.T) {
		w := doJSONRequest(t, wired.Router, http.MethodGet, "/metrics", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("an organization's own API key is rejected — this endpoint is admin-only", func(t *testing.T) {
		adminAuth := "Bearer " + support.AdminAPIKey
		var org organizationDTO
		w := doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/", adminAuth, `{"name":"Acme"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("create org status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
			t.Fatalf("decode org: %v", err)
		}
		var orgKey issuedKeyDTO
		w = doJSONRequest(t, wired.Router, http.MethodPost, "/api/v1/organizations/"+org.ID+"/api-keys", adminAuth, "")
		if w.Code != http.StatusCreated {
			t.Fatalf("issue key status = %d, want %d; body=%s", w.Code, http.StatusCreated, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &orgKey); err != nil {
			t.Fatalf("decode key: %v", err)
		}

		w = doJSONRequest(t, wired.Router, http.MethodGet, "/metrics", "Bearer "+orgKey.Key, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("the installation admin key is accepted", func(t *testing.T) {
		status, _ := scrapeMetrics(t, wired)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want %d", status, http.StatusOK)
		}
	})
}
