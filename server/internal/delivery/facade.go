package delivery

import (
	"context"
	"math/rand"
	"time"

	"beecon/internal/httpx"
	"beecon/internal/metrics"
	"beecon/internal/organizations"
)

// defaultListLimit and maxListLimit bound ListEvents' page size (PD10-style)
// when a caller supplies none, or supplies one larger than Beecon allows.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// claimBatchLimit bounds how many due events one DispatchOnce call claims
// at a time — an internal constant (FD5: only the five spec-named
// BEECON_* vars are configurable), not a hard cap on outbox throughput
// (the next scan tick, or a Nudge, picks up whatever's left).
const claimBatchLimit = 50

// leaseMargin pads the claim lease past the delivery timeout itself
// (section 3 of the architecture doc: "2x BEECON_DELIVERY_TIMEOUT +
// margin"), so a slow-but-still-in-flight attempt is never re-claimed by a
// second instance before it can finish and release its own lease.
const leaseMargin = 5 * time.Second

// Facade is the delivery module's only public surface.
type Facade struct {
	repo            Repository
	workQueue       WorkQueue
	secrets         SecretIssuer
	caller          EndpointCaller
	recorder        Recorder
	metrics         *metrics.Registry
	outboxStats     OutboxStats
	newEndpointID   func() string
	newEventID      func() string
	deliveryTimeout time.Duration
	jitter          func() float64
	nudge           func()
	now             func() time.Time
}

// NewFacade wires the facade with its driven Repository, the
// installation-level WorkQueue DispatchOnce claims through, the
// access-provided SecretIssuer port, the EndpointCaller driven adapter, the
// consumer-defined Recorder port (nil is a silent no-op), injected id
// minters, BEECON_DELIVERY_TIMEOUT, and a clock so tests can supply
// deterministic ids and a fixed time. Jitter defaults to math/rand.Float64
// (WithJitter overrides it for deterministic tests); nudge defaults to a
// no-op (WithNudge wires it to the dispatcher loop's wake, so Enqueue's
// first attempt runs promptly instead of waiting out the scan interval).
func NewFacade(
	repo Repository,
	workQueue WorkQueue,
	secrets SecretIssuer,
	caller EndpointCaller,
	recorder Recorder,
	newEndpointID func() string,
	newEventID func() string,
	deliveryTimeout time.Duration,
	now func() time.Time,
) *Facade {
	return &Facade{
		repo:            repo,
		workQueue:       workQueue,
		secrets:         secrets,
		caller:          caller,
		recorder:        recorder,
		newEndpointID:   newEndpointID,
		newEventID:      newEventID,
		deliveryTimeout: deliveryTimeout,
		jitter:          rand.Float64,
		nudge:           func() {},
		now:             now,
	}
}

// WithJitter overrides the ±10% retry-schedule jitter (schedule.go's
// NextAttempt) with a deterministic source, e.g. func() float64 { return
// 0.5 } for exactly-on-schedule tests.
func (f *Facade) WithJitter(jitter func() float64) *Facade {
	f.jitter = jitter
	return f
}

// WithNudge wires the dispatcher loop's wake (worker.Group.Nudge) so
// Enqueue's and Redeliver's first attempt runs promptly instead of waiting
// out the scan interval (PD30's "immediately"). A facade built without one
// (NewFacade's default) simply never nudges anything — the scan interval
// still picks the event up.
func (f *Facade) WithNudge(nudge func()) *Facade {
	f.nudge = nudge
	return f
}

// WithMetrics wires this facade's Prometheus recording (PD38d): every
// delivery attempt's outcome, by event type and result. A facade built
// without one (the nil zero value NewFacade leaves it at) makes every
// metrics call a silent no-op, exactly like a nil Recorder already does for
// logging.
func (f *Facade) WithMetrics(registry *metrics.Registry) *Facade {
	f.metrics = registry
	return f
}

// WithOutboxStats wires the installation-level OutboxStats port
// OutboxPendingDepth/OutboxOldestPendingAge delegate to (PD38d). A Facade
// built without one (this never called) makes both return a zero value and
// no error — harmless for every caller today (only the outbox metrics gauges
// call them).
func (f *Facade) WithOutboxStats(stats OutboxStats) *Facade {
	f.outboxStats = stats
	return f
}

// OutboxPendingDepth returns how many outbox events are currently PENDING
// (PD38d) — the outbox-depth metrics gauge's data source, queried fresh at
// every scrape (a GaugeFunc) rather than maintained as a running counter.
func (f *Facade) OutboxPendingDepth(ctx context.Context) (int, error) {
	if f.outboxStats == nil {
		return 0, nil
	}
	depth, _, err := f.outboxStats.PendingDepthAndOldestAge(ctx, f.now())
	return depth, err
}

// OutboxOldestPendingAge returns the age of the oldest PENDING outbox event
// (zero when there are none) — PD38d's oldest-pending-age metrics gauge.
func (f *Facade) OutboxOldestPendingAge(ctx context.Context) (time.Duration, error) {
	if f.outboxStats == nil {
		return 0, nil
	}
	_, age, err := f.outboxStats.PendingDepthAndOldestAge(ctx, f.now())
	return age, err
}

// SetEndpointResult is SetEndpoint's response: the endpoint's identity, its
// current URL and creation time, and — only on first creation — the freshly
// minted secret (PD31: "the signing secret is generated server-side and
// returned exactly once at creation; URL changes keep the secret").
type SetEndpointResult struct {
	ID        EndpointID
	URL       string
	CreatedAt time.Time
	Secret    string
}

// SetEndpoint sets org's single webhook endpoint URL (PD31). A first call
// for org mints a fresh signing secret (returned exactly once, in Secret);
// a later call that only changes the URL keeps the existing secret (Secret
// is empty). url must be an absolute http(s) URL.
func (f *Facade) SetEndpoint(ctx context.Context, org organizations.OrgID, url string) (SetEndpointResult, error) {
	if err := ValidateEndpointURL(url); err != nil {
		return SetEndpointResult{}, err
	}
	existing, err := f.repo.FindEndpoint(ctx, org)
	if err != nil {
		return SetEndpointResult{}, err
	}
	if existing != nil {
		return f.updateEndpointURL(ctx, *existing, url)
	}
	return f.createEndpoint(ctx, org, url)
}

func (f *Facade) updateEndpointURL(ctx context.Context, existing Endpoint, url string) (SetEndpointResult, error) {
	updated := Endpoint{ID: existing.ID, OrgID: existing.OrgID, URL: url, CreatedAt: existing.CreatedAt}
	if err := f.repo.SaveEndpoint(ctx, updated); err != nil {
		return SetEndpointResult{}, err
	}
	return SetEndpointResult{ID: updated.ID, URL: updated.URL, CreatedAt: updated.CreatedAt}, nil
}

func (f *Facade) createEndpoint(ctx context.Context, org organizations.OrgID, url string) (SetEndpointResult, error) {
	issued, err := f.secrets.IssueWebhookSecret(ctx, org)
	if err != nil {
		return SetEndpointResult{}, err
	}
	endpoint := Endpoint{ID: EndpointID(f.newEndpointID()), OrgID: org, URL: url, CreatedAt: f.now()}
	if err := f.repo.SaveEndpoint(ctx, endpoint); err != nil {
		return SetEndpointResult{}, err
	}
	return SetEndpointResult{ID: endpoint.ID, URL: endpoint.URL, CreatedAt: endpoint.CreatedAt, Secret: issued.Secret}, nil
}

// EndpointView is GetEndpoint's response: URL, secret prefix, and creation
// date — never the full secret, which is also unrecoverable from a
// database dump (PD31).
type EndpointView struct {
	ID           EndpointID
	URL          string
	SecretPrefix string
	CreatedAt    time.Time
}

// GetEndpoint returns org's configured webhook endpoint. An org with none
// configured yet is not-found.
func (f *Facade) GetEndpoint(ctx context.Context, org organizations.OrgID) (EndpointView, error) {
	endpoint, err := f.repo.FindEndpoint(ctx, org)
	if err != nil {
		return EndpointView{}, err
	}
	if endpoint == nil {
		return EndpointView{}, ErrNotFound()
	}
	prefix, err := f.secrets.WebhookSecretPrefix(ctx, org)
	if err != nil {
		return EndpointView{}, err
	}
	return EndpointView{ID: endpoint.ID, URL: endpoint.URL, SecretPrefix: prefix, CreatedAt: endpoint.CreatedAt}, nil
}

// RotateSecretResult is RotateSecret's response: the new secret, returned
// exactly once.
type RotateSecretResult struct {
	Secret           string
	Prefix           string
	OverlapExpiresAt time.Time
}

// RotateSecret mints a fresh webhook signing secret for org's endpoint
// (PD31, mirroring PD23): deliveries during the overlap window carry
// signatures from both secrets. An org with no configured endpoint is
// rejected with a validation error — there is nothing to rotate.
func (f *Facade) RotateSecret(ctx context.Context, org organizations.OrgID, overlapHours *int) (RotateSecretResult, error) {
	endpoint, err := f.repo.FindEndpoint(ctx, org)
	if err != nil {
		return RotateSecretResult{}, err
	}
	if endpoint == nil {
		return RotateSecretResult{}, ErrNoEndpoint()
	}
	rotated, err := f.secrets.RotateWebhookSecret(ctx, org, overlapHours)
	if err != nil {
		return RotateSecretResult{}, err
	}
	return RotateSecretResult{Secret: rotated.Secret, Prefix: rotated.Prefix, OverlapExpiresAt: rotated.OverlapExpiresAt}, nil
}

// SendTest enqueues a signed webhook.test event at org's configured
// endpoint, proving the channel works before anything real depends on it.
// An org with no configured endpoint is rejected with a validation error
// (rather than silently landing NO_ENDPOINT, unlike every other event
// type) — requesting a test delivery only makes sense once there is
// somewhere to deliver it.
func (f *Facade) SendTest(ctx context.Context, org organizations.OrgID) (Event, error) {
	endpoint, err := f.repo.FindEndpoint(ctx, org)
	if err != nil {
		return Event{}, err
	}
	if endpoint == nil {
		return Event{}, ErrNoEndpoint()
	}
	return f.Enqueue(ctx, org, EventTypeWebhookTest, map[string]any{})
}

// Enqueue marshals the PD32 envelope exactly once and persists it (the
// outbox's own durability guarantee: an event accepted just before a
// restart is still delivered after it). An org with no configured
// endpoint gets Status NO_ENDPOINT and zero attempts (FD7); otherwise the
// event is PENDING with its first attempt due immediately (PD30), and the
// dispatcher loop is nudged so that first attempt runs promptly.
func (f *Facade) Enqueue(ctx context.Context, org organizations.OrgID, eventType string, data any) (Event, error) {
	now := f.now()
	id := EventID(f.newEventID())
	body, err := marshalEnvelope(id, eventType, data, now)
	if err != nil {
		return Event{}, err
	}

	endpoint, err := f.repo.FindEndpoint(ctx, org)
	if err != nil {
		return Event{}, err
	}
	status := StatusPending
	if endpoint == nil {
		status = StatusNoEndpoint
	}

	event := Event{
		ID:            id,
		OrgID:         org,
		Type:          eventType,
		Body:          body,
		Status:        status,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	if err := f.repo.SaveEvent(ctx, event); err != nil {
		return Event{}, err
	}
	if status == StatusPending {
		f.nudge()
	}
	return event, nil
}

// ListEventsParams is ListEvents' caller-facing filter shape: Type and/or
// DeliveryStatus optionally narrow the page (empty means unrestricted);
// Cursor is the opaque cursor a consumer sends back exactly as a previous
// page's NextCursor returned it.
type ListEventsParams struct {
	Type           string
	DeliveryStatus string
	Cursor         string
	Limit          int
}

// ListEventsResult is one cursor-paginated page of Events, newest first;
// NextCursor is empty when this was the last page.
type ListEventsResult struct {
	Items      []Event
	NextCursor string
}

// ListEvents returns a page of org's outbox events, optionally narrowed by
// type and/or delivery status, newest first.
func (f *Facade) ListEvents(ctx context.Context, org organizations.OrgID, params ListEventsParams) (ListEventsResult, error) {
	cursor, err := decodeEventCursor(params.Cursor)
	if err != nil {
		return ListEventsResult{}, err
	}
	limit := normalizeListLimit(params.Limit)

	items, err := f.repo.ListEventsPage(ctx, org, ListFilter{
		Type:           params.Type,
		DeliveryStatus: Status(params.DeliveryStatus),
		Cursor:         cursor,
		Limit:          limit + 1,
	})
	if err != nil {
		return ListEventsResult{}, err
	}
	return paginateEvents(items, limit), nil
}

// Redeliver re-queues event id for another delivery attempt: same evt_ id
// and body (PD32's idempotency guarantee holds across manual redelivery
// too), status PENDING, next attempt due immediately. Works regardless of
// the event's current status — including NO_ENDPOINT (FD7: an event that
// landed undeliverable because no endpoint existed yet can be redelivered
// manually once one is configured) and FAILED (retry exhaustion). An
// unknown id, or one belonging to another organization, is not-found.
func (f *Facade) Redeliver(ctx context.Context, org organizations.OrgID, id EventID) (Event, error) {
	event, err := f.repo.FindEvent(ctx, org, id)
	if err != nil {
		return Event{}, err
	}
	if event == nil {
		return Event{}, ErrNotFound()
	}
	requeued := *event
	requeued.Status = StatusPending
	requeued.NextAttemptAt = f.now()
	if err := f.repo.SaveEvent(ctx, requeued); err != nil {
		return Event{}, err
	}
	f.nudge()
	return requeued, nil
}

// DispatchOnce claims a batch of due events (across every organization —
// WorkQueue is deliberately installation-level, PD29) and attempts each
// exactly once. It is the dispatcher worker.Loop's Run func: production
// calls it on a schedule (app/workers.go), tests call it directly
// (worker.Group.RunOnce) after arranging state and travelling the shared
// clock.
func (f *Facade) DispatchOnce(ctx context.Context) error {
	now := f.now()
	leaseTTL := 2*f.deliveryTimeout + leaseMargin
	events, err := f.workQueue.ClaimDue(ctx, now, leaseTTL, claimBatchLimit)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := f.dispatchOne(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// dispatchOne makes exactly one delivery attempt for event, signs it with
// every currently active secret (1-2 during a rotation's overlap window),
// applies PD30's outcome (DELIVERED, rescheduled, or FAILED at
// MaxAttempts), persists that outcome, and always writes one attempt log
// entry (the AC's "every delivery attempt", success or failure).
func (f *Facade) dispatchOne(ctx context.Context, event Event) error {
	endpoint, err := f.repo.FindEndpoint(ctx, event.OrgID)
	if err != nil {
		return err
	}
	if endpoint == nil {
		// No endpoint to deliver to (e.g. deleted after this event was
		// enqueued — not a case any API exposes today, but harmless): leave
		// the event exactly as claimed; its lease simply expires and the row
		// is re-claimed next scan.
		return nil
	}
	secrets, err := f.secrets.ActiveWebhookSecrets(ctx, event.OrgID)
	if err != nil {
		return err
	}

	attemptNumber := event.Attempts + 1
	started := f.now()
	status, postErr := f.signAndPost(ctx, endpoint.URL, event, started, secrets)
	duration := f.now().Sub(started)

	updated := applyAttemptOutcome(event, status, postErr, f.now(), attemptNumber, f.jitter)
	if err := f.repo.SaveEvent(ctx, updated); err != nil {
		return err
	}
	return f.recordAttempt(ctx, event, attemptNumber, status, duration)
}

// signAndPost signs event with every currently active secret and POSTs it,
// treating a signing failure exactly like a failed POST: status stays 0
// (no response was ever attempted) and the error flows into
// applyAttemptOutcome's normal failure/reschedule/FAILED path — a
// malformed secret (never true for a Beecon-minted one, but Sign fails
// loudly rather than silently mis-signing) still counts as one attempt and
// still writes one attempt log entry, it just never reaches the network.
func (f *Facade) signAndPost(ctx context.Context, url string, event Event, attemptedAt time.Time, secrets []string) (status int, err error) {
	headers, err := Sign(event.ID, attemptedAt, event.Body, secrets)
	if err != nil {
		return 0, err
	}
	return f.caller.Post(ctx, url, headers.Headers(), event.Body, f.deliveryTimeout)
}

// applyAttemptOutcome derives the Event's post-attempt state: DELIVERED on
// a 2xx response, otherwise a bumped attempt count that either reschedules
// (PD30's jittered backoff table) or, at MaxAttempts, marks FAILED.
func applyAttemptOutcome(event Event, status int, postErr error, now time.Time, attemptNumber int, jitter func() float64) Event {
	updated := event
	lastAttempt := now
	updated.LastAttemptAt = &lastAttempt
	updated.Attempts = attemptNumber

	if postErr == nil && isSuccessStatus(status) {
		updated.Status = StatusDelivered
		return updated
	}
	if IsExhausted(updated.Attempts) {
		updated.Status = StatusFailed
		return updated
	}
	updated.Status = StatusPending
	updated.NextAttemptAt = NextAttempt(now, updated.Attempts, jitter)
	return updated
}

func isSuccessStatus(status int) bool {
	return status >= 200 && status < 300
}

func (f *Facade) recordAttempt(ctx context.Context, event Event, attemptNumber, status int, duration time.Duration) error {
	f.recordAttemptMetric(event.Type, status)
	if f.recorder == nil {
		return nil
	}
	return f.recorder.Record(ctx, LogEntry{
		OrgID:      event.OrgID,
		EventID:    string(event.ID),
		EventType:  event.Type,
		Attempt:    attemptNumber,
		Status:     status,
		DurationMs: duration.Milliseconds(),
	})
}

// recordAttemptMetric records PD38d's delivery-attempt counter, by event
// type and whether this attempt reached a 2xx response.
func (f *Facade) recordAttemptMetric(eventType string, status int) {
	if f.metrics == nil {
		return
	}
	f.metrics.RecordDeliveryAttempt(eventType, isSuccessStatus(status))
}

func paginateEvents(items []Event, limit int) ListEventsResult {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := ListEventsResult{Items: items}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = encodeEventCursor(last.CreatedAt, last.ID)
	}
	return result
}

func normalizeListLimit(requested int) int {
	if requested <= 0 {
		return defaultListLimit
	}
	if requested > maxListLimit {
		return maxListLimit
	}
	return requested
}

func encodeEventCursor(createdAt time.Time, id EventID) string {
	return httpx.EncodeCursor(createdAt.UTC().Format(time.RFC3339Nano), string(id))
}

func decodeEventCursor(raw string) (*ListCursor, error) {
	fields, err := httpx.DecodeCursor(raw, 2)
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	if fields == nil {
		return nil, nil
	}
	createdAt, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, ErrInvalidCursor()
	}
	return &ListCursor{CreatedAt: createdAt, ID: EventID(fields[1])}, nil
}
