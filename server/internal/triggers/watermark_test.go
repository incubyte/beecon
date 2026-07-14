// Package triggers_test (watermark half): ApplyWatermark/Pause/Resume are
// pure (types.go's own doc comment — no I/O, no clock injection needed
// beyond a plain time.Time parameter), so this file drives them directly
// with hand-built TriggerInstance/PollRecord fixtures — no facade, no
// repository, no fakes. poll_test.go covers the orchestration around these
// decisions (claiming, fetching, persisting); this file covers the decision
// itself (PD34).
package triggers_test

import (
	"testing"
	"time"

	"beecon/internal/triggers"
)

var watermarkTestNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func recordAt(id string, ts time.Time) triggers.PollRecord {
	return triggers.PollRecord{ID: id, Timestamp: ts, Payload: map[string]any{"id": id}}
}

func freshInstance() triggers.TriggerInstance {
	return triggers.TriggerInstance{ID: "trg_1", TriggerSlug: "outlook-message-received"}
}

// --- Baseline poll (PD34 AC2: "the first poll after create establishes the
// baseline and delivers nothing historical") ---

func TestApplyWatermark_BaselinePollWithExistingRecordsSetsWatermarkToTheNewestTimestampAndFiresNothing(t *testing.T) {
	instance := freshInstance() // WatermarkAt is nil: never polled before
	records := []triggers.PollRecord{
		recordAt("msg-1", watermarkTestNow.Add(-2*time.Hour)),
		recordAt("msg-2", watermarkTestNow.Add(-1*time.Hour)),
	}

	outcome := triggers.ApplyWatermark(instance, records, watermarkTestNow)

	if len(outcome.ToFire) != 0 {
		t.Fatalf("ToFire = %+v, want none — a baseline poll must deliver nothing historical", outcome.ToFire)
	}
	if outcome.Instance.WatermarkAt == nil || !outcome.Instance.WatermarkAt.Equal(watermarkTestNow.Add(-1*time.Hour)) {
		t.Errorf("WatermarkAt = %v, want the newest existing record's timestamp %v", outcome.Instance.WatermarkAt, watermarkTestNow.Add(-1*time.Hour))
	}
}

func TestApplyWatermark_BaselinePollWithZeroRecordsSetsWatermarkToNow(t *testing.T) {
	instance := freshInstance()

	outcome := triggers.ApplyWatermark(instance, nil, watermarkTestNow)

	if len(outcome.ToFire) != 0 {
		t.Fatalf("ToFire = %+v, want none", outcome.ToFire)
	}
	if outcome.Instance.WatermarkAt == nil || !outcome.Instance.WatermarkAt.Equal(watermarkTestNow) {
		t.Errorf("WatermarkAt = %v, want now (%v) — a baseline poll with no existing records has nothing to set it from", outcome.Instance.WatermarkAt, watermarkTestNow)
	}
}

func TestApplyWatermark_BaselinePollRecordsSeenIDsAtTheBoundaryTimestampSoAReDeliveredRecordIsNotDoubleFired(t *testing.T) {
	instance := freshInstance()
	boundary := watermarkTestNow.Add(-1 * time.Hour)
	records := []triggers.PollRecord{recordAt("msg-1", boundary)}
	baseline := triggers.ApplyWatermark(instance, records, watermarkTestNow).Instance

	// The very next (non-baseline) poll re-delivers the same record at the
	// exact boundary timestamp (a provider's own "newer than" filter may be
	// inclusive) — it must not fire, since it was already accounted for at
	// the baseline.
	outcome := triggers.ApplyWatermark(baseline, []triggers.PollRecord{recordAt("msg-1", boundary)}, watermarkTestNow)

	if len(outcome.ToFire) != 0 {
		t.Fatalf("ToFire = %+v, want none — the baseline's own boundary record must not re-fire", outcome.ToFire)
	}
}

// --- Subsequent polls (PD34 AC3: "each subsequent poll emits one
// trigger.event per new record, exactly once") ---

func TestApplyWatermark_SubsequentPollFiresOnlyRecordsStrictlyNewerThanTheWatermark(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	older := recordAt("msg-old", watermark.Add(-time.Minute))
	newer := recordAt("msg-new", watermark.Add(time.Minute))

	outcome := triggers.ApplyWatermark(instance, []triggers.PollRecord{older, newer}, watermarkTestNow)

	if len(outcome.ToFire) != 1 || outcome.ToFire[0].ID != "msg-new" {
		t.Fatalf("ToFire = %+v, want exactly [msg-new]", outcome.ToFire)
	}
}

func TestApplyWatermark_SubsequentPollAdvancesTheWatermarkToTheNewestFiredRecord(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	newest := watermark.Add(2 * time.Minute)
	records := []triggers.PollRecord{recordAt("msg-a", watermark.Add(time.Minute)), recordAt("msg-b", newest)}

	outcome := triggers.ApplyWatermark(instance, records, watermarkTestNow)

	if outcome.Instance.WatermarkAt == nil || !outcome.Instance.WatermarkAt.Equal(newest) {
		t.Errorf("WatermarkAt = %v, want the newest fired record's timestamp %v", outcome.Instance.WatermarkAt, newest)
	}
}

func TestApplyWatermark_ToFireIsOrderedOldestFirst(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	records := []triggers.PollRecord{
		recordAt("msg-newer", watermark.Add(2*time.Minute)),
		recordAt("msg-older", watermark.Add(time.Minute)),
	}

	outcome := triggers.ApplyWatermark(instance, records, watermarkTestNow)

	if len(outcome.ToFire) != 2 || outcome.ToFire[0].ID != "msg-older" || outcome.ToFire[1].ID != "msg-newer" {
		t.Fatalf("ToFire = %+v, want [msg-older, msg-newer] (chronological order)", outcome.ToFire)
	}
}

// --- Boundary tie-break (PD34: "a record exactly at the watermark whose id
// was not already seen there") ---

func TestApplyWatermark_ARecordExactlyAtTheWatermarkWithAnUnseenIDFires(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	instance.SeenIDs = []string{"msg-already-seen"}

	outcome := triggers.ApplyWatermark(instance, []triggers.PollRecord{recordAt("msg-new-at-boundary", watermark)}, watermarkTestNow)

	if len(outcome.ToFire) != 1 || outcome.ToFire[0].ID != "msg-new-at-boundary" {
		t.Fatalf("ToFire = %+v, want exactly [msg-new-at-boundary] — an unseen id at the exact watermark must fire", outcome.ToFire)
	}
}

func TestApplyWatermark_ARecordExactlyAtTheWatermarkWithASeenIDDoesNotFireAgain(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	instance.SeenIDs = []string{"msg-already-seen"}

	outcome := triggers.ApplyWatermark(instance, []triggers.PollRecord{recordAt("msg-already-seen", watermark)}, watermarkTestNow)

	if len(outcome.ToFire) != 0 {
		t.Fatalf("ToFire = %+v, want none — a record already in SeenIDs at the exact watermark must never fire twice", outcome.ToFire)
	}
}

func TestApplyWatermark_WhenTheWatermarkDoesNotMoveSeenIDsAccumulateRatherThanReset(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	instance.SeenIDs = []string{"msg-first"}

	outcome := triggers.ApplyWatermark(instance, []triggers.PollRecord{recordAt("msg-second", watermark)}, watermarkTestNow)

	if outcome.Instance.WatermarkAt == nil || !outcome.Instance.WatermarkAt.Equal(watermark) {
		t.Fatalf("WatermarkAt = %v, want unchanged at %v — no record exceeded it", outcome.Instance.WatermarkAt, watermark)
	}
	seen := map[string]bool{}
	for _, id := range outcome.Instance.SeenIDs {
		seen[id] = true
	}
	if !seen["msg-first"] || !seen["msg-second"] {
		t.Fatalf("SeenIDs = %v, want both msg-first (already there) and msg-second (newly fired) accumulated", outcome.Instance.SeenIDs)
	}
}

func TestApplyWatermark_WhenTheWatermarkAdvancesSeenIDsResetToJustTheNewBoundary(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	instance.SeenIDs = []string{"msg-old-boundary"}
	newBoundary := watermark.Add(time.Minute)

	outcome := triggers.ApplyWatermark(instance, []triggers.PollRecord{recordAt("msg-new-boundary", newBoundary)}, watermarkTestNow)

	if len(outcome.Instance.SeenIDs) != 1 || outcome.Instance.SeenIDs[0] != "msg-new-boundary" {
		t.Fatalf("SeenIDs = %v, want exactly [msg-new-boundary] — the old boundary's seen-ids are already excluded by the timestamp comparison alone", outcome.Instance.SeenIDs)
	}
}

// --- Pause / Resume (Slice 4: connection-leaves-ACTIVE pause, FD6's
// pause-resume-skips-the-gap on resume) ---

func TestPause_SetsPausedAtAndLeavesWatermarkAndSeenIDsUntouched(t *testing.T) {
	watermark := watermarkTestNow.Add(-1 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &watermark
	instance.SeenIDs = []string{"msg-1"}

	paused := triggers.Pause(instance, watermarkTestNow)

	if paused.PausedAt == nil || !paused.PausedAt.Equal(watermarkTestNow) {
		t.Errorf("PausedAt = %v, want %v", paused.PausedAt, watermarkTestNow)
	}
	if paused.WatermarkAt == nil || !paused.WatermarkAt.Equal(watermark) {
		t.Errorf("WatermarkAt = %v, want unchanged at %v — Pause must not touch poll state", paused.WatermarkAt, watermark)
	}
	if len(paused.SeenIDs) != 1 || paused.SeenIDs[0] != "msg-1" {
		t.Errorf("SeenIDs = %v, want unchanged", paused.SeenIDs)
	}
}

func TestResume_ClearsThePauseAndResetsTheWatermarkToNowSkippingTheGap(t *testing.T) {
	oldWatermark := watermarkTestNow.Add(-48 * time.Hour)
	pausedAt := watermarkTestNow.Add(-24 * time.Hour)
	instance := freshInstance()
	instance.WatermarkAt = &oldWatermark
	instance.SeenIDs = []string{"msg-1"}
	instance.PausedAt = &pausedAt

	resumed := triggers.Resume(instance, watermarkTestNow)

	if resumed.PausedAt != nil {
		t.Errorf("PausedAt = %v, want nil", resumed.PausedAt)
	}
	if resumed.WatermarkAt == nil || !resumed.WatermarkAt.Equal(watermarkTestNow) {
		t.Errorf("WatermarkAt = %v, want reset to now (%v) — FD6: pause-resume skips the gap", resumed.WatermarkAt, watermarkTestNow)
	}
	if resumed.SeenIDs != nil {
		t.Errorf("SeenIDs = %v, want cleared", resumed.SeenIDs)
	}
}
