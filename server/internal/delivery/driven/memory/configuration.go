package memory

import (
	"fmt"
	"sync/atomic"
	"time"

	"beecon/internal/delivery"
)

var fixedTestTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// defaultTestDeliveryTimeout is the BEECON_DELIVERY_TIMEOUT stand-in when
// Overrides doesn't supply its own — long enough that no fake
// EndpointCaller in a unit test could plausibly hit it.
const defaultTestDeliveryTimeout = 10 * time.Second

// defaultTestEndpointCap and defaultTestAutoDisableThreshold are the
// BEECON_WEBHOOK_ENDPOINT_CAP/BEECON_ENDPOINT_AUTODISABLE_FAILURES
// stand-ins when Overrides doesn't supply its own (Slice 8, PD45) —
// mirror config.go's own production defaults.
const (
	defaultTestEndpointCap          = 5
	defaultTestAutoDisableThreshold = 5
)

// Overrides configures NewFacadeWithOverrides. Repository/WorkQueue,
// NewEndpointID, NewEventID, DeliveryTimeout, Jitter, and Now fall back to
// a deterministic in-memory default when left zero-valued. Secrets,
// Caller, and Recorder are the narrow ports delivery.Facade depends on
// (BOUNDARIES: delivery depends on access) with no default — callers
// supply a fake (or access's own memory-backed facade, and a scripted
// EndpointCaller) directly, the same way app/wiring.go composes them in
// production.
type Overrides struct {
	Repository           delivery.Repository
	WorkQueue            delivery.WorkQueue
	OutboxStats          delivery.OutboxStats
	Secrets              delivery.SecretIssuer
	Caller               delivery.EndpointCaller
	Recorder             delivery.Recorder
	NewEndpointID        func() string
	NewEventID           func() string
	DeliveryTimeout      time.Duration
	EndpointCap          int
	AutoDisableThreshold int
	Jitter               func() float64
	Now                  func() time.Time
}

// NewFacadeWithOverrides builds a delivery.Facade backed by the in-memory
// Repository unless a fake is supplied, with deterministic ids, a fixed
// clock, and zero jitter unless overridden.
func NewFacadeWithOverrides(o Overrides) *delivery.Facade {
	repository := o.Repository
	workQueue := o.WorkQueue
	outboxStats := o.OutboxStats
	if repository == nil || workQueue == nil || outboxStats == nil {
		shared := NewRepository()
		if repository == nil {
			repository = shared
		}
		if workQueue == nil {
			workQueue = shared
		}
		if outboxStats == nil {
			outboxStats = shared
		}
	}
	newEndpointID := o.NewEndpointID
	if newEndpointID == nil {
		newEndpointID = sequentialIDs("wep_")
	}
	newEventID := o.NewEventID
	if newEventID == nil {
		newEventID = sequentialIDs("evt_")
	}
	deliveryTimeout := o.DeliveryTimeout
	if deliveryTimeout <= 0 {
		deliveryTimeout = defaultTestDeliveryTimeout
	}
	endpointCap := o.EndpointCap
	if endpointCap <= 0 {
		endpointCap = defaultTestEndpointCap
	}
	autoDisableThreshold := o.AutoDisableThreshold
	if autoDisableThreshold <= 0 {
		autoDisableThreshold = defaultTestAutoDisableThreshold
	}
	now := o.Now
	if now == nil {
		now = func() time.Time { return fixedTestTime }
	}

	facade := delivery.NewFacade(repository, workQueue, o.Secrets, o.Caller, o.Recorder, newEndpointID, newEventID, deliveryTimeout, endpointCap, autoDisableThreshold, now)
	if o.Jitter != nil {
		facade = facade.WithJitter(o.Jitter)
	} else {
		facade = facade.WithJitter(func() float64 { return 0.5 })
	}
	facade = facade.WithOutboxStats(outboxStats)
	return facade
}

func sequentialIDs(prefix string) func() string {
	var n int64
	return func() string {
		return fmt.Sprintf("%s%d", prefix, atomic.AddInt64(&n, 1))
	}
}
