// poll.go is PD34's poll-tick orchestration: PollOnce claims due
// TriggerInstances (across every organization, PollQueue) and, per
// instance, decides whether to pause, resume, or actually poll — leaning on
// watermark.go's pure ApplyWatermark/Pause/Resume for every decision that
// does not need a provider call or a repository write.
package triggers

import (
	"context"
	"time"

	"beecon/internal/connections"
)

// pollClaimBatchLimit bounds how many due instances one PollOnce call
// claims at a time — an internal constant (FD5: only the five spec-named
// BEECON_* vars are configurable), not a hard cap on polling throughput
// (the next scan tick picks up whatever's left).
const pollClaimBatchLimit = 50

// pollLeaseTTL is the poller's own claim lease (section 3 of the
// architecture doc: "poll: 60s") — comfortably above one tick's worst-case
// work (one provider call plus a handful of outbox enqueues), so a crash
// mid-poll is safely re-claimed by the next instance rather than stuck
// forever, and a poll run never overlaps itself (PD34).
const pollLeaseTTL = 60 * time.Second

// EventTypeTriggerEvent is PD32's fired-event type — triggers' own copy of
// delivery.EventTypeTriggerEvent's literal value (BOUNDARIES: triggers does
// not depend on delivery; the EventSink port is the seam, so the literal is
// deliberately duplicated here rather than imported).
const EventTypeTriggerEvent = "trigger.event"

// PollOnce claims a batch of due TriggerInstances (across every
// organization — PollQueue is deliberately installation-level, PD29) and
// polls each exactly once. It is the poller worker.Loop's Run func:
// production calls it on a schedule (app/workers.go), tests call it
// directly (worker.Group.RunOnce) after arranging state and travelling the
// shared clock. A single instance's poll failure (a provider error, a
// rate limit, or an unexpected repository read) is caught and logged
// per-instance (PD34's own AC) — only a failure to persist an instance's
// own advanced state after a successful attempt aborts the batch early, the
// same "infrastructure failure, not a domain outcome" line delivery's own
// DispatchOnce draws.
func (f *Facade) PollOnce(ctx context.Context) error {
	now := f.now()
	instances, err := f.pollQueue.ClaimDuePolls(ctx, now, pollLeaseTTL, pollClaimBatchLimit)
	if err != nil {
		f.recordPollRun(false)
		return err
	}
	for _, instance := range instances {
		if err := f.pollOne(ctx, instance, now); err != nil {
			f.recordPollRun(false)
			return err
		}
	}
	f.recordPollRun(true)
	return nil
}

// recordPollRun records PD38d's trigger poll-run counter: success unless
// PollOnce itself aborted early (a claim or state-persistence failure — see
// PollOnce's own doc comment on which failures propagate).
func (f *Facade) recordPollRun(success bool) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordTriggerPollRun(success)
}

// pollOne decides and executes one instance's poll tick: pause when its
// connection has left ACTIVE, resume when it has rejoined ACTIVE after
// being paused, otherwise poll for real. Every branch reschedules the
// instance's NextPollAt at the trigger definition's own interval (floored
// at pollMinInterval), so a paused instance keeps re-checking its
// connection on the same cadence it would otherwise poll at.
func (f *Facade) pollOne(ctx context.Context, instance TriggerInstance, now time.Time) error {
	interval := f.resolveInstancePollInterval(ctx, instance)

	connection, err := f.connections.Get(ctx, instance.OrgID, instance.ConnectionID)
	if err != nil {
		return f.reschedulePollFailure(ctx, instance, now, interval, err)
	}
	if connection.Status != connections.StatusActive {
		return f.repo.Save(ctx, reschedule(Pause(instance, now), now, interval))
	}
	if instance.PausedAt != nil {
		return f.repo.Save(ctx, reschedule(Resume(instance, now), now, interval))
	}
	return f.pollActive(ctx, instance, now, interval)
}

// pollActive fetches records via RecordSource, applies PD34's watermark
// decision, emits one trigger.event per newly fired record, and persists
// the instance's advanced watermark/seen-ids/schedule — in that order, so a
// crash between emitting events and saving simply results in the same
// records being re-fetched and re-evaluated against the still-unmoved
// watermark next tick (harmless: ApplyWatermark is idempotent given the
// same input state).
func (f *Facade) pollActive(ctx context.Context, instance TriggerInstance, now time.Time, interval time.Duration) error {
	watermark := time.Time{}
	if instance.WatermarkAt != nil {
		watermark = *instance.WatermarkAt
	}
	records, err := f.recordSource.FetchRecords(ctx, PollRecordQuery{
		OrgID:        instance.OrgID,
		UserID:       instance.UserID,
		ConnectionID: instance.ConnectionID,
		TriggerSlug:  instance.TriggerSlug,
		Config:       instance.Config,
		Watermark:    watermark,
	})
	if err != nil {
		return f.reschedulePollFailure(ctx, instance, now, interval, err)
	}

	outcome := ApplyWatermark(instance, records, now)
	if err := f.emitFiredEvents(ctx, outcome.Instance, outcome.ToFire); err != nil {
		return err
	}
	return f.repo.Save(ctx, reschedule(outcome.Instance, now, interval))
}

// emitFiredEvents enqueues one trigger.event per fired record (PD32's data
// shape: triggerInstanceId, triggerSlug, connectionId, userId, payload),
// oldest first (records already arrives in that order, PollOutcome's own
// contract).
func (f *Facade) emitFiredEvents(ctx context.Context, instance TriggerInstance, records []PollRecord) error {
	for _, record := range records {
		data := map[string]any{
			"triggerInstanceId": string(instance.ID),
			"triggerSlug":       instance.TriggerSlug,
			"connectionId":      string(instance.ConnectionID),
			"userId":            string(instance.UserID),
			"payload":           record.Payload,
		}
		if err := f.events.Enqueue(ctx, instance.OrgID, EventTypeTriggerEvent, data); err != nil {
			return err
		}
		f.recordEventEmitted(instance.TriggerSlug)
	}
	return nil
}

// recordEventEmitted records PD38d's trigger-events-emitted counter, by
// trigger slug.
func (f *Facade) recordEventEmitted(triggerSlug string) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordTriggerEventEmitted(triggerSlug)
}

// reschedulePollFailure writes a poll-failure log entry (PD34's own AC —
// nothing is logged on success) and reschedules the instance at interval
// without touching its watermark/seen-ids — a failed tick observed nothing,
// so there is nothing to advance.
func (f *Facade) reschedulePollFailure(ctx context.Context, instance TriggerInstance, now time.Time, interval time.Duration, cause error) error {
	f.logPollFailure(ctx, instance, cause)
	return f.repo.Save(ctx, reschedule(instance, now, interval))
}

func (f *Facade) logPollFailure(ctx context.Context, instance TriggerInstance, cause error) {
	if f.recorder == nil {
		return
	}
	_ = f.recorder.Record(ctx, LogEntry{
		OrgID:             instance.OrgID,
		TriggerInstanceID: instance.ID,
		TriggerSlug:       instance.TriggerSlug,
		ConnectionID:      instance.ConnectionID,
		Error:             cause.Error(),
	})
}

// resolveInstancePollInterval looks up instance's trigger definition for
// its own (already boot-clamped, PD28) PollIntervalSeconds, floored at
// pollMinInterval (BEECON_POLL_MIN_INTERVAL); an unexpected lookup failure
// (the slug is immutable once an instance exists, so this is not a normal
// path) falls back to pollMinInterval rather than leaving the instance
// unscheduled.
func (f *Facade) resolveInstancePollInterval(ctx context.Context, instance TriggerInstance) time.Duration {
	definition, err := f.definitions.TriggerDefinitionDetail(ctx, instance.TriggerSlug)
	if err != nil {
		return f.pollMinInterval
	}
	interval := time.Duration(definition.PollIntervalSeconds) * time.Second
	if interval < f.pollMinInterval {
		return f.pollMinInterval
	}
	return interval
}

// reschedule returns a copy of instance with NextPollAt advanced to
// now+interval.
func reschedule(instance TriggerInstance, now time.Time, interval time.Duration) TriggerInstance {
	rescheduled := instance
	next := now.Add(interval)
	rescheduled.NextPollAt = &next
	return rescheduled
}
