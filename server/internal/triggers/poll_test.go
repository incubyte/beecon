// Package triggers_test (poll orchestration half): exercises PollOnce
// against the in-memory Repository/PollQueue and hand-written fakes for the
// three Slice 4 ports (RecordSource, EventSink, Recorder) — reuses
// fakeDefinitionReader/newFakeDefinitionReader, fakeConnectionReader/
// newFakeConnectionReader/activeConnection, assertDomainError, testOrg,
// testUser, outlookMessageReceivedSlug/outlookMessageReceivedDefinition
// declared in facade_test.go (same package). watermark_test.go covers the
// pure ApplyWatermark/Pause/Resume decisions this file's orchestration
// leans on; test/crucial_path/trigger_polling_journey_integration_test.go
// covers the same story end to end against the real composition root.
package triggers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"beecon/internal/catalog"
	"beecon/internal/connections"
	"beecon/internal/organizations"
	"beecon/internal/triggers"
	memory "beecon/internal/triggers/driven/memory"
)

// fakeRecordSource is a hand-written triggers.RecordSource: scripted per
// trigger slug, so tests can model a successful fetch (a fixed set of
// records) or a poll failure (a plain error) without a real execution
// facade. Every call is recorded so tests can assert whether the provider
// was reached at all (e.g. never, on a paused tick) and with which
// watermark.
type fakeRecordSource struct {
	recordsBySlug map[string][]triggers.PollRecord
	errBySlug     map[string]error
	calls         []triggers.PollRecordQuery
}

func (f *fakeRecordSource) FetchRecords(_ context.Context, query triggers.PollRecordQuery) ([]triggers.PollRecord, error) {
	f.calls = append(f.calls, query)
	if err, ok := f.errBySlug[query.TriggerSlug]; ok {
		return nil, err
	}
	return f.recordsBySlug[query.TriggerSlug], nil
}

// fakeEventSink is a hand-written triggers.EventSink: records every enqueued
// event, optionally scripted to fail.
type fakeEventSink struct {
	events []fakeEnqueuedEvent
	err    error
}

type fakeEnqueuedEvent struct {
	org       organizations.OrgID
	eventType string
	data      any
}

func (f *fakeEventSink) Enqueue(_ context.Context, org organizations.OrgID, eventType string, data any) error {
	f.events = append(f.events, fakeEnqueuedEvent{org: org, eventType: eventType, data: data})
	return f.err
}

// fakeTriggersRecorder is a hand-written triggers.Recorder: records every
// poll-failure log entry handed to it (PD34: only a failing poll writes
// one).
type fakeTriggersRecorder struct {
	entries []triggers.LogEntry
}

func (f *fakeTriggersRecorder) Record(_ context.Context, entry triggers.LogEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

// pollTestClock lets PollOnce tests travel time deterministically (claiming
// due instances, floor math, resume timestamps) without a real sleep.
type pollTestClock struct{ now time.Time }

func (c *pollTestClock) Now() time.Time { return c.now }
func (c *pollTestClock) Advance(d time.Duration) time.Time {
	c.now = c.now.Add(d)
	return c.now
}

// pollTestFixture bundles everything one PollOnce test needs: the facade, an
// active connection callers can flip non-ACTIVE, and the three polling
// fakes.
type pollTestFixture struct {
	facade     *triggers.Facade
	clock      *pollTestClock
	connReader *fakeConnectionReader
	connID     connections.ConnectionID
	recordSrc  *fakeRecordSource
	events     *fakeEventSink
	recorder   *fakeTriggersRecorder
	instance   triggers.TriggerInstance
}

// newPollTestFixture creates one ACTIVE connection and one trigger instance
// bound to it (born via Create, exactly like production), wired with
// definition (whose own PollIntervalSeconds/ConfigSchema/PayloadSchema the
// caller controls) and pollMinInterval.
func newPollTestFixture(t *testing.T, definition catalog.TriggerDefinitionSummary, pollMinInterval time.Duration) *pollTestFixture {
	t.Helper()
	clock := &pollTestClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	connID := connections.ConnectionID("conn_1")
	connReader := &fakeConnectionReader{byKey: map[connectionKey]connections.Connection{
		{org: testOrg, id: connID}: activeConnection(connID, testOrg, testUser),
	}}
	recordSrc := &fakeRecordSource{recordsBySlug: map[string][]triggers.PollRecord{}, errBySlug: map[string]error{}}
	events := &fakeEventSink{}
	recorder := &fakeTriggersRecorder{}

	facade := memory.NewFacadeWithOverrides(memory.Overrides{
		Definitions:     newFakeDefinitionReader(definition),
		Connections:     connReader,
		RecordSource:    recordSrc,
		Events:          events,
		Recorder:        recorder,
		PollMinInterval: pollMinInterval,
		Now:             clock.Now,
	})

	instance, err := facade.Create(context.Background(), testOrg, triggers.CreateParams{
		ConnectionID: connID, TriggerSlug: definition.Slug, Config: map[string]any{"folderId": "Inbox"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	return &pollTestFixture{
		facade: facade, clock: clock, connReader: connReader, connID: connID,
		recordSrc: recordSrc, events: events, recorder: recorder, instance: instance,
	}
}

func (f *pollTestFixture) setConnectionStatus(status connections.Status) {
	conn := f.connReader.byKey[connectionKey{org: testOrg, id: f.connID}]
	conn.Status = status
	f.connReader.byKey[connectionKey{org: testOrg, id: f.connID}] = conn
}

func (f *pollTestFixture) get(t *testing.T) triggers.TriggerInstance {
	t.Helper()
	got, err := f.facade.Get(context.Background(), testOrg, f.instance.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got
}

func definitionWithInterval(seconds int) catalog.TriggerDefinitionSummary {
	definition := outlookMessageReceivedDefinition()
	definition.PollIntervalSeconds = seconds
	return definition
}

// --- Pause on every non-ACTIVE status (PD33/PD34: "polling pauses
// automatically and produces no failed-poll noise") ---

func TestPollOnce_PausesOnEveryNonActiveConnectionStatusAndWritesNoLogEntry(t *testing.T) {
	for _, status := range []connections.Status{connections.StatusInitiated, connections.StatusExpired, connections.StatusDisconnected} {
		t.Run(string(status), func(t *testing.T) {
			fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
			fixture.setConnectionStatus(status)

			if err := fixture.facade.PollOnce(context.Background()); err != nil {
				t.Fatalf("PollOnce: %v", err)
			}

			got := fixture.get(t)
			if got.PausedAt == nil {
				t.Fatal("PausedAt is nil, want it set — a non-ACTIVE connection must pause the instance")
			}
			if len(fixture.recorder.entries) != 0 {
				t.Fatalf("recorder.entries = %+v, want none — pausing must produce no failed-poll log noise", fixture.recorder.entries)
			}
			if len(fixture.recordSrc.calls) != 0 {
				t.Fatalf("RecordSource was called %d times, want 0 — a paused instance must never reach the provider", len(fixture.recordSrc.calls))
			}
			if len(fixture.events.events) != 0 {
				t.Fatalf("events enqueued = %+v, want none", fixture.events.events)
			}
			if got.NextPollAt == nil || !got.NextPollAt.After(fixture.clock.now) {
				t.Errorf("NextPollAt = %v, want rescheduled after now (%v) — a paused instance keeps re-checking on the same cadence", got.NextPollAt, fixture.clock.now)
			}
		})
	}
}

// TestPollOnce_PauseLeavesWatermarkAndSeenIDsUntouched pins watermark.go's
// own Pause contract at the orchestration level: pausing must not disturb
// poll state, since nothing fired.
func TestPollOnce_PauseLeavesWatermarkAndSeenIDsUntouched(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-1", Timestamp: fixture.clock.now.Add(-time.Minute), Payload: map[string]any{"id": "msg-1"}},
	}
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // baseline poll, sets watermark
		t.Fatalf("baseline PollOnce: %v", err)
	}
	baselineWatermark := fixture.get(t).WatermarkAt
	fixture.clock.Advance(time.Hour)
	fixture.setConnectionStatus(connections.StatusExpired)

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got := fixture.get(t)
	if got.WatermarkAt == nil || !got.WatermarkAt.Equal(*baselineWatermark) {
		t.Errorf("WatermarkAt = %v, want unchanged at %v", got.WatermarkAt, baselineWatermark)
	}
}

// --- Resume (PD33/PD34, FD6: "completing a reconnect resumes it", watermark
// reset — skip the gap) ---

func TestPollOnce_ResumeAfterConnectionRejoinsActiveClearsThePauseAndResetsTheWatermarkWithoutFetching(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	fixture.setConnectionStatus(connections.StatusExpired)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // pauses
		t.Fatalf("pausing PollOnce: %v", err)
	}
	fixture.clock.Advance(time.Hour)
	fixture.setConnectionStatus(connections.StatusActive)

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("resuming PollOnce: %v", err)
	}

	got := fixture.get(t)
	if got.PausedAt != nil {
		t.Errorf("PausedAt = %v, want nil after resume", got.PausedAt)
	}
	if got.WatermarkAt == nil || !got.WatermarkAt.Equal(fixture.clock.now) {
		t.Errorf("WatermarkAt = %v, want reset to the resume tick's now (%v)", got.WatermarkAt, fixture.clock.now)
	}
	if len(fixture.recordSrc.calls) != 0 {
		t.Errorf("RecordSource was called %d times, want 0 — a resume tick only resets state, it does not also fetch in the same tick", len(fixture.recordSrc.calls))
	}
}

// TestPollOnce_RecordsFromWhileDisabledAreSkippedButNewRecordsAfterResumeFire
// is FD6/PD34's full "skip the gap" story: a record that only ever appears
// while the connection is non-ACTIVE never fires, even once the connection
// rejoins ACTIVE and polling resumes.
func TestPollOnce_RecordsThatArriveWhileNonActiveAreSkippedButNewRecordsAfterResumeFire(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // baseline
		t.Fatalf("baseline PollOnce: %v", err)
	}
	fixture.clock.Advance(time.Minute)
	fixture.setConnectionStatus(connections.StatusExpired)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // pauses
		t.Fatalf("pausing PollOnce: %v", err)
	}
	// A record "arrives" while the connection is away — RecordSource would
	// serve it if ever asked, but the paused tick never calls it.
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-during-outage", Timestamp: fixture.clock.now, Payload: map[string]any{"id": "msg-during-outage"}},
	}
	fixture.clock.Advance(time.Minute)
	fixture.setConnectionStatus(connections.StatusActive)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // resumes, resets watermark to this tick's now
		t.Fatalf("resuming PollOnce: %v", err)
	}
	fixture.clock.Advance(time.Hour)
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-during-outage", Timestamp: fixture.clock.now.Add(-90 * time.Minute), Payload: map[string]any{"id": "msg-during-outage"}}, // still older than the reset watermark
		{ID: "msg-after-resume", Timestamp: fixture.clock.now, Payload: map[string]any{"id": "msg-after-resume"}},
	}

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("final PollOnce: %v", err)
	}

	if len(fixture.events.events) != 1 {
		t.Fatalf("events enqueued = %+v, want exactly 1 (msg-after-resume only)", fixture.events.events)
	}
	data, ok := fixture.events.events[0].data.(map[string]any)
	if !ok {
		t.Fatalf("event data = %T, want map[string]any", fixture.events.events[0].data)
	}
	payload, _ := data["payload"].(map[string]any)
	if payload["id"] != "msg-after-resume" {
		t.Fatalf("fired payload id = %v, want %q — the record that arrived during the outage must never fire", payload["id"], "msg-after-resume")
	}
}

// --- Poll failure (PD34: "poll failures... log and wait for the next tick
// without killing the schedule") ---

func TestPollOnce_AFailingFetchWritesALogEntryAndReschedulesWithoutAdvancingTheWatermark(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-1", Timestamp: fixture.clock.now.Add(-time.Minute), Payload: map[string]any{"id": "msg-1"}},
	}
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // baseline, sets watermark
		t.Fatalf("baseline PollOnce: %v", err)
	}
	watermarkBeforeFailure := fixture.get(t).WatermarkAt
	fixture.clock.Advance(time.Hour)
	fixture.recordSrc.errBySlug[outlookMessageReceivedSlug] = errors.New("graph returned 503")

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce itself must not fail the batch on one instance's poll error: %v", err)
	}

	if len(fixture.recorder.entries) != 1 {
		t.Fatalf("recorder.entries = %+v, want exactly 1", fixture.recorder.entries)
	}
	entry := fixture.recorder.entries[0]
	if entry.OrgID != testOrg || entry.ConnectionID != fixture.connID || entry.TriggerSlug != outlookMessageReceivedSlug {
		t.Errorf("entry = %+v, want OrgID/ConnectionID/TriggerSlug matching the failing instance", entry)
	}
	if entry.Error == "" {
		t.Error("entry.Error is empty, want the poll failure's message")
	}
	got := fixture.get(t)
	if got.WatermarkAt == nil || !got.WatermarkAt.Equal(*watermarkBeforeFailure) {
		t.Errorf("WatermarkAt = %v, want unchanged at %v — a failed tick observed nothing", got.WatermarkAt, watermarkBeforeFailure)
	}
	if got.NextPollAt == nil || !got.NextPollAt.After(fixture.clock.now) {
		t.Errorf("NextPollAt = %v, want rescheduled after now (%v) — a failure must not stop the schedule", got.NextPollAt, fixture.clock.now)
	}
	if len(fixture.events.events) != 0 {
		t.Errorf("events enqueued = %+v, want none on a failed fetch", fixture.events.events)
	}
}

// TestPollOnce_TheTickAfterAFailurePollsNormally is PD34's other half: the
// schedule is not killed — the very next tick succeeds and fires normally.
func TestPollOnce_TheTickAfterAFailurePollsNormally(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // baseline
		t.Fatalf("baseline PollOnce: %v", err)
	}
	fixture.clock.Advance(time.Minute)
	fixture.recordSrc.errBySlug[outlookMessageReceivedSlug] = errors.New("graph returned 503")
	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("failing PollOnce: %v", err)
	}
	delete(fixture.recordSrc.errBySlug, outlookMessageReceivedSlug)
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-recovered", Timestamp: fixture.clock.now, Payload: map[string]any{"id": "msg-recovered"}},
	}
	fixture.clock.Advance(time.Minute)

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("recovering PollOnce: %v", err)
	}

	if len(fixture.events.events) != 1 {
		t.Fatalf("events enqueued = %+v, want exactly 1 once the provider recovers", fixture.events.events)
	}
}

// --- next_poll_at floor (PD28/PD34: definition intervals below the
// platform minimum are floored) ---

func TestPollOnce_ReschedulesAtThePollMinIntervalWhenTheDefinitionsIntervalIsLower(t *testing.T) {
	fixture := newPollTestFixture(t, definitionWithInterval(10), 30*time.Second)

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got := fixture.get(t)
	want := fixture.clock.now.Add(30 * time.Second)
	if got.NextPollAt == nil || !got.NextPollAt.Equal(want) {
		t.Errorf("NextPollAt = %v, want floored to the platform minimum %v (not the definition's own 10s)", got.NextPollAt, want)
	}
}

func TestPollOnce_HonorsADefinitionsIntervalAtOrAboveThePollMinInterval(t *testing.T) {
	fixture := newPollTestFixture(t, definitionWithInterval(120), 30*time.Second)

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got := fixture.get(t)
	want := fixture.clock.now.Add(120 * time.Second)
	if got.NextPollAt == nil || !got.NextPollAt.Equal(want) {
		t.Errorf("NextPollAt = %v, want the definition's own 120s interval honored (%v)", got.NextPollAt, want)
	}
}

// --- Happy path: no log noise on success (PD34's own contrast with the
// failure path) ---

func TestPollOnce_ASuccessfulPollFiresNewRecordsAndWritesNoLogEntry(t *testing.T) {
	fixture := newPollTestFixture(t, outlookMessageReceivedDefinition(), 30*time.Second)
	if err := fixture.facade.PollOnce(context.Background()); err != nil { // baseline
		t.Fatalf("baseline PollOnce: %v", err)
	}
	fixture.clock.Advance(time.Minute)
	fixture.recordSrc.recordsBySlug[outlookMessageReceivedSlug] = []triggers.PollRecord{
		{ID: "msg-new", Timestamp: fixture.clock.now, Payload: map[string]any{"id": "msg-new", "subject": "Hello"}},
	}

	if err := fixture.facade.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	if len(fixture.events.events) != 1 {
		t.Fatalf("events enqueued = %+v, want exactly 1", fixture.events.events)
	}
	fired := fixture.events.events[0]
	if fired.eventType != triggers.EventTypeTriggerEvent {
		t.Errorf("eventType = %q, want %q", fired.eventType, triggers.EventTypeTriggerEvent)
	}
	data, ok := fired.data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map[string]any", fired.data)
	}
	if data["triggerInstanceId"] != string(fixture.instance.ID) {
		t.Errorf("data.triggerInstanceId = %v, want %q", data["triggerInstanceId"], fixture.instance.ID)
	}
	if data["connectionId"] != string(fixture.connID) {
		t.Errorf("data.connectionId = %v, want %q", data["connectionId"], fixture.connID)
	}
	if data["userId"] != string(testUser) {
		t.Errorf("data.userId = %v, want %q", data["userId"], testUser)
	}
	if data["triggerSlug"] != outlookMessageReceivedSlug {
		t.Errorf("data.triggerSlug = %v, want %q", data["triggerSlug"], outlookMessageReceivedSlug)
	}
	if len(fixture.recorder.entries) != 0 {
		t.Fatalf("recorder.entries = %+v, want none — a successful poll writes no log entry", fixture.recorder.entries)
	}
}

// --- ClaimDuePolls lease semantics (PD29/PD34: "a poll run never overlaps
// itself") — exercised directly against the PollQueue port the memory
// adapter provides, mirroring the real dual-dialect claim query's contract
// (also pinned against real SQLite in driven/bun/repository_test.go). ---

func TestClaimDuePolls_ALeasedInstanceIsNotReclaimedUntilItsLeaseExpires(t *testing.T) {
	repo := memory.NewRepository()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	due := now
	instance := triggers.TriggerInstance{
		ID: "trg_1", OrgID: testOrg, Status: triggers.StatusActive, NextPollAt: &due, CreatedAt: now,
	}
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}

	first, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("first ClaimDuePolls: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first claim = %+v, want exactly the one due instance", first)
	}

	second, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("second ClaimDuePolls: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second claim (before the lease expires, before any Save) = %+v, want none — a leased instance must not be claimed twice", second)
	}

	afterLease := now.Add(61 * time.Second)
	third, err := repo.ClaimDuePolls(context.Background(), afterLease, 60*time.Second, 10)
	if err != nil {
		t.Fatalf("third ClaimDuePolls: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("third claim (after the lease expired) = %+v, want the instance reclaimable again", third)
	}
}

// TestClaimDuePolls_NeverClaimsADisabledInstance pins PollQueue's own doc
// comment: "a DISABLED instance is never claimed — disable stops firing
// applies to polling too."
func TestClaimDuePolls_NeverClaimsADisabledInstance(t *testing.T) {
	repo := memory.NewRepository()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	instance := triggers.TriggerInstance{
		ID: "trg_1", OrgID: testOrg, Status: triggers.StatusDisabled, NextPollAt: &now, CreatedAt: now,
	}
	if err := repo.Save(context.Background(), instance); err != nil {
		t.Fatalf("Save: %v", err)
	}

	claimed, err := repo.ClaimDuePolls(context.Background(), now, 60*time.Second, 10)

	if err != nil {
		t.Fatalf("ClaimDuePolls: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %+v, want none — a DISABLED instance must never be polled", claimed)
	}
}
