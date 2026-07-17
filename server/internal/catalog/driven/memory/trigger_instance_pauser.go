package memory

import (
	"context"
	"sync"

	"beecon/internal/catalog"
)

// TriggerInstancePauser is an in-memory catalog.TriggerInstancePauser for
// tests (Phase 5 registry sub-phase Slice 4, PD66): records every trigger
// slug Activate reports as removed, so a test can assert which slugs were
// paused without wiring a real triggers.Facade. FailOnSlug optionally makes
// this fake return an error for one specific slug instead of recording it —
// the seam a rollback test drives to prove a failed pause aborts the whole
// activation atomically.
type TriggerInstancePauser struct {
	mu       sync.Mutex
	paused   []string
	failOn   string
	failWith error
}

var _ catalog.TriggerInstancePauser = (*TriggerInstancePauser)(nil)

func NewTriggerInstancePauser() *TriggerInstancePauser {
	return &TriggerInstancePauser{}
}

// FailOnSlug makes a subsequent PauseInstancesForRemovedTrigger call for
// triggerSlug return err instead of recording it.
func (p *TriggerInstancePauser) FailOnSlug(triggerSlug string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failOn = triggerSlug
	p.failWith = err
}

// Paused returns every trigger slug recorded so far, in call order.
func (p *TriggerInstancePauser) Paused() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	paused := make([]string, len(p.paused))
	copy(paused, p.paused)
	return paused
}

func (p *TriggerInstancePauser) PauseInstancesForRemovedTrigger(_ context.Context, triggerSlug string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failOn != "" && p.failOn == triggerSlug {
		return p.failWith
	}
	p.paused = append(p.paused, triggerSlug)
	return nil
}
