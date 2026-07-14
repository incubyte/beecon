// watermark.go is PD34's pure poll-tick decision logic: given everything a
// RecordSource just fetched for one instance, which of those records are
// genuinely new, and what the instance's watermark/seen-id state becomes
// next. Nothing here does I/O — poll.go is the only caller, and every
// function takes its clock as a plain time.Time parameter, so this is
// exhaustively unit-testable without a fake clock or a fake anything.
package triggers

import (
	"sort"
	"time"
)

// PollOutcome is ApplyWatermark's result: ToFire are the fetched records
// that are genuinely new since the instance's last poll (PD34) — sorted
// oldest first, so events fire in the chronological order the provider
// reported them — and Instance is the same instance with its watermark and
// seen-ids advanced to reflect having processed every fetched record, not
// just the ones that fired.
type PollOutcome struct {
	ToFire   []PollRecord
	Instance TriggerInstance
}

// ApplyWatermark is PD34's pure poll-tick decision: the first poll after an
// instance's watermark is unset is the baseline poll (AC2 of Slice 4) — it
// establishes the watermark from whatever currently exists and fires
// nothing, however many records were fetched. Every later poll fires every
// record strictly newer than the watermark, plus a record exactly at the
// watermark whose id was not already seen there (the boundary tie-break a
// plain timestamp comparison alone cannot resolve, since a provider's own
// "newer than" filter may be inclusive) — and advances the watermark and
// seen-ids to match (AC3: the same record never fires twice).
func ApplyWatermark(instance TriggerInstance, records []PollRecord, now time.Time) PollOutcome {
	sorted := sortedByTimestamp(records)
	if instance.WatermarkAt == nil {
		return baselinePoll(instance, sorted, now)
	}
	return advancePoll(instance, sorted)
}

// baselinePoll establishes instance's watermark from the newest record
// currently observed (or now, when the provider returned none) and fires
// nothing (PD34's "baseline poll delivers nothing historical") — seen-ids
// are still recorded at that boundary timestamp, so a record sharing it
// that is re-delivered on the very next (non-baseline) poll is correctly
// recognized as already accounted for.
func baselinePoll(instance TriggerInstance, sorted []PollRecord, now time.Time) PollOutcome {
	watermark := now
	var seenIDs []string
	if latest, ok := latestTimestamp(sorted); ok {
		watermark = latest
		seenIDs = idsAtTimestamp(sorted, latest)
	}
	advanced := instance
	advanced.WatermarkAt = &watermark
	advanced.SeenIDs = seenIDs
	return PollOutcome{Instance: advanced}
}

// advancePoll fires every record strictly newer than instance's current
// watermark, plus a record exactly at the watermark whose id is not already
// in instance.SeenIDs (the boundary tie-break). When the watermark advances,
// seen-ids reset to just the records now sitting at the new boundary
// timestamp — anything older is already excluded by the timestamp
// comparison alone; when the watermark does not move (no record exceeded
// it), seen-ids instead accumulate the newly fired at-boundary records
// alongside the ones already remembered there.
func advancePoll(instance TriggerInstance, sorted []PollRecord) PollOutcome {
	watermark := *instance.WatermarkAt
	seen := toIDSet(instance.SeenIDs)

	var toFire []PollRecord
	for _, record := range sorted {
		if record.Timestamp.After(watermark) || (record.Timestamp.Equal(watermark) && !seen[record.ID]) {
			toFire = append(toFire, record)
		}
	}

	newWatermark := watermark
	if latest, ok := latestTimestamp(toFire); ok && latest.After(watermark) {
		newWatermark = latest
	}

	newSeen := seen
	if !newWatermark.Equal(watermark) {
		newSeen = map[string]bool{}
	}
	for _, record := range toFire {
		if record.Timestamp.Equal(newWatermark) {
			newSeen[record.ID] = true
		}
	}

	advanced := instance
	advanced.WatermarkAt = &newWatermark
	advanced.SeenIDs = fromIDSet(newSeen)
	return PollOutcome{ToFire: toFire, Instance: advanced}
}

// Pause returns a copy of instance marked paused at now (Slice 4, PD33's
// "a connection leaving ACTIVE pauses its instances automatically"): poll
// state (watermark, seen-ids) is left untouched — nothing has fired, and
// PD34/FD6's reset happens at Resume, not at Pause, mirroring
// TriggerInstance.Disable/Enable's own explicit-disable pair.
func Pause(instance TriggerInstance, now time.Time) TriggerInstance {
	paused := instance
	paused.PausedAt = &now
	return paused
}

// Resume returns a copy of instance with its pause cleared and its
// watermark reset to now (FD6: "pause-resume skips the gap" — a
// reconnected connection's instance never delivers records that arrived
// while it was away, the same semantics an explicit Disable/Enable pair
// applies via TriggerInstance.Enable).
func Resume(instance TriggerInstance, now time.Time) TriggerInstance {
	resumed := instance
	resumed.PausedAt = nil
	resumed.WatermarkAt = &now
	resumed.SeenIDs = nil
	return resumed
}

// sortedByTimestamp returns a copy of records ordered oldest-first
// (timestamp ascending, id ascending as a deterministic tiebreaker) —
// PollOutcome.ToFire's own order, and the order every helper below assumes.
func sortedByTimestamp(records []PollRecord) []PollRecord {
	sorted := make([]PollRecord, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Timestamp.Equal(sorted[j].Timestamp) {
			return sorted[i].Timestamp.Before(sorted[j].Timestamp)
		}
		return sorted[i].ID < sorted[j].ID
	})
	return sorted
}

// latestTimestamp returns the newest timestamp among records, and false
// when records is empty.
func latestTimestamp(records []PollRecord) (time.Time, bool) {
	if len(records) == 0 {
		return time.Time{}, false
	}
	latest := records[0].Timestamp
	for _, record := range records[1:] {
		if record.Timestamp.After(latest) {
			latest = record.Timestamp
		}
	}
	return latest, true
}

// idsAtTimestamp returns the ids of every record whose timestamp equals ts.
func idsAtTimestamp(records []PollRecord, ts time.Time) []string {
	var ids []string
	for _, record := range records {
		if record.Timestamp.Equal(ts) {
			ids = append(ids, record.ID)
		}
	}
	return ids
}

func toIDSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// fromIDSet returns set's members as a sorted slice (deterministic
// persistence — a re-Save of an unchanged set produces byte-identical
// stored state), or nil for an empty set.
func fromIDSet(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
