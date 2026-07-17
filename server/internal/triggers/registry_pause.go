// registry_pause.go is the triggers module's half of the Phase 5 registry
// sub-phase's dependent-trigger-instance safety net (Slice 4, PD66):
// PauseInstancesForRemovedTrigger satisfies catalog.TriggerInstancePauser
// through a composition-root adapter (app/recorders.go) — triggers itself
// never imports catalog's activation path directly here; catalog calls this
// method through the port it defines, mirroring RecordSource/EventSink's own
// consumer-defined-port shape for the opposite module pairing (BOUNDARIES:
// triggers already depends on catalog, but the dependency this file
// participates in runs the other way — catalog reaching into triggers — so
// the port lives on catalog's side and this is simply the method the
// composition root's adapter forwards to).
package triggers

import "context"

// PauseInstancesForRemovedTrigger transitions every organization's
// TriggerInstance bound to triggerSlug to StatusPausedTriggerRemoved
// (Phase 5 registry sub-phase, Slice 4, PD66): called through the
// catalog.TriggerInstancePauser port when a catalog activation removes a
// trigger definition those instances depend on — the instance is paused
// with a clear, self-explanatory status rather than silently dropped or
// left to keep polling a trigger definition that no longer exists. An
// instance already StatusPausedTriggerRemoved is left untouched (idempotent:
// the same trigger slug reported removed more than once, or a retried
// activation, never re-writes a row that needs no change). A facade with no
// TriggerSlugIndex wired (WithTriggerSlugIndex never called) treats this as
// a no-op — there is nothing yet to pause without it.
func (f *Facade) PauseInstancesForRemovedTrigger(ctx context.Context, triggerSlug string) error {
	if f.triggerSlugIndex == nil {
		return nil
	}
	instances, err := f.triggerSlugIndex.ListByTriggerSlug(ctx, triggerSlug)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if instance.Status == StatusPausedTriggerRemoved {
			continue
		}
		if err := f.repo.Save(ctx, instance.PauseForRemovedTrigger()); err != nil {
			return err
		}
	}
	return nil
}
