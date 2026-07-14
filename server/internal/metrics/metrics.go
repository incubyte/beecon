// Package metrics owns Beecon's Prometheus registry (PD24): the operability
// signals the reviewer asked for before production dependence — tool
// execution counts and durations by provider and status, upstream rate-limit
// retries, OAuth handshake outcomes, and token-refresh outcomes. Phase 3
// Slice 7 (PD38d) extends it with the background-processing signals that
// make the outbox, poller, and refresh scheduler observable before anyone
// depends on them: connections-by-status, outbox depth/oldest-pending-age,
// delivery-attempt outcomes, trigger poll runs/events emitted, and
// scheduled-refresh outcomes. It is shared infrastructure (BOUNDARIES:
// importable by any module, imports no domain module), like idgen, httpx, or
// vault — injected concretely into the facades that record against it
// rather than behind a port, since there is exactly one implementation
// (YAGNI, architecture Flagged Decision 3). Still one /metrics endpoint
// (handler.go), never a second one.
package metrics

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	outcomeSuccess = "success"
	outcomeFailure = "failure"
)

// unreachableStatusLabel is RecordToolExecution's status label for an
// attempt that never got an HTTP response at all (a network failure
// reaching the provider).
const unreachableStatusLabel = "unreachable"

// connectionStatuses is metrics' own copy of connections.Status's fixed
// lifecycle values (BOUNDARIES: metrics imports no domain module, so the
// connections-by-status gauge's label set is a plain string literal here,
// the same deliberate duplication triggers/delivery already use for the
// PD32 event-type literals across their own module boundary).
var connectionStatuses = []string{"INITIATED", "ACTIVE", "EXPIRED", "DISCONNECTED"}

// Registry is Beecon's single Prometheus registry. New builds exactly one;
// the composition root wires it into every facade that records against it
// and serves it at GET /metrics (handler.go).
type Registry struct {
	registry              *prometheus.Registry
	toolExecutions        *prometheus.CounterVec
	toolExecutionDuration *prometheus.HistogramVec
	rateLimitRetries      *prometheus.CounterVec
	oauthHandshakes       *prometheus.CounterVec
	tokenRefreshes        *prometheus.CounterVec

	deliveryAttempts         *prometheus.CounterVec
	triggerPollRuns          *prometheus.CounterVec
	triggerEventsEmitted     *prometheus.CounterVec
	scheduledRefreshOutcomes *prometheus.CounterVec
}

// New builds a Registry carrying only Beecon's own metrics — a dedicated
// prometheus.Registry rather than the global default, so GET /metrics
// exposes exactly PD24/PD38d's signals. The connections-by-status and outbox
// gauges are registered separately (RegisterConnectionsByStatusGauge,
// RegisterOutboxGauges) once the facades they scrape exist — New is called
// before any facade is built (app/wiring.go), so it cannot yet hold a
// function value to call at scrape time.
func New() *Registry {
	r := &Registry{
		registry: prometheus.NewRegistry(),
		toolExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_tool_executions_total",
			Help: "Tool execution attempts by provider and upstream status.",
		}, []string{"provider", "status"}),
		toolExecutionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "beecon_tool_execution_duration_seconds",
			Help: "Tool execution attempt duration in seconds by provider and upstream status.",
		}, []string{"provider", "status"}),
		rateLimitRetries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_rate_limit_retries_total",
			Help: "Retries issued against a normalized upstream rate limit, by provider.",
		}, []string{"provider"}),
		oauthHandshakes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_oauth_handshakes_total",
			Help: "OAuth authorization_code handshake outcomes, by provider.",
		}, []string{"provider", "outcome"}),
		tokenRefreshes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_token_refreshes_total",
			Help: "Refresh-token grant outcomes, by provider.",
		}, []string{"provider", "outcome"}),
		deliveryAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_delivery_attempts_total",
			Help: "Webhook delivery attempts by event type and result.",
		}, []string{"type", "result"}),
		triggerPollRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_trigger_poll_runs_total",
			Help: "Trigger poller PollOnce runs by result.",
		}, []string{"result"}),
		triggerEventsEmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_trigger_events_emitted_total",
			Help: "trigger.event deliveries enqueued by trigger slug.",
		}, []string{"triggerSlug"}),
		scheduledRefreshOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "beecon_scheduled_refresh_outcomes_total",
			Help: "Background refresh-scheduler outcomes by result.",
		}, []string{"outcome"}),
	}
	r.registry.MustRegister(
		r.toolExecutions,
		r.toolExecutionDuration,
		r.rateLimitRetries,
		r.oauthHandshakes,
		r.tokenRefreshes,
		r.deliveryAttempts,
		r.triggerPollRuns,
		r.triggerEventsEmitted,
		r.scheduledRefreshOutcomes,
	)
	return r
}

// RecordToolExecution records one upstream tool-call attempt's outcome and
// duration (PD24): status is the provider's HTTP status code, or
// unreachableStatusLabel when the provider could not be reached at all.
func (r *Registry) RecordToolExecution(provider string, status int, duration time.Duration) {
	label := statusLabel(status)
	r.toolExecutions.WithLabelValues(provider, label).Inc()
	r.toolExecutionDuration.WithLabelValues(provider, label).Observe(duration.Seconds())
}

func statusLabel(status int) string {
	if status == 0 {
		return unreachableStatusLabel
	}
	return strconv.Itoa(status)
}

// RecordRateLimitRetry counts one retry PD21's retry policy issued against a
// normalized upstream rate limit, by provider.
func (r *Registry) RecordRateLimitRetry(provider string) {
	r.rateLimitRetries.WithLabelValues(provider).Inc()
}

// RecordOAuthHandshake counts one authorization_code token-exchange outcome
// (PD24), by provider.
func (r *Registry) RecordOAuthHandshake(provider string, success bool) {
	r.oauthHandshakes.WithLabelValues(provider, outcomeLabel(success)).Inc()
}

// RecordTokenRefresh counts one refresh_token grant outcome (PD24), by
// provider.
func (r *Registry) RecordTokenRefresh(provider string, success bool) {
	r.tokenRefreshes.WithLabelValues(provider, outcomeLabel(success)).Inc()
}

func outcomeLabel(success bool) string {
	if success {
		return outcomeSuccess
	}
	return outcomeFailure
}

// RecordDeliveryAttempt counts one webhook delivery attempt (PD38d), by
// event type and whether it reached a 2xx response.
func (r *Registry) RecordDeliveryAttempt(eventType string, success bool) {
	r.deliveryAttempts.WithLabelValues(eventType, outcomeLabel(success)).Inc()
}

// RecordTriggerPollRun counts one poller PollOnce invocation (PD38d): success
// unless claiming or persisting an instance's advanced state failed
// outright (a poll failure for a single instance is not itself a PollOnce
// failure — see triggers.Facade.PollOnce's own doc comment).
func (r *Registry) RecordTriggerPollRun(success bool) {
	r.triggerPollRuns.WithLabelValues(outcomeLabel(success)).Inc()
}

// RecordTriggerEventEmitted counts one trigger.event enqueued for delivery
// (PD38d), by trigger slug.
func (r *Registry) RecordTriggerEventEmitted(triggerSlug string) {
	r.triggerEventsEmitted.WithLabelValues(triggerSlug).Inc()
}

// ScheduledRefreshOutcome names one RefreshDueOnce per-connection outcome
// (PD38d): Refreshed for a completed or already-fresh grant, Expired when
// the provider permanently refused it, Error for a transient failure retried
// on a later scan.
const (
	ScheduledRefreshOutcomeRefreshed = "refreshed"
	ScheduledRefreshOutcomeExpired   = "expired"
	ScheduledRefreshOutcomeError     = "error"
)

// RecordScheduledRefreshOutcome counts one background refresh-scheduler
// outcome (PD38d, distinct from RecordTokenRefresh's own provider-keyed
// counter, which fires for both scheduled and PD18 request-path refreshes
// alike): outcome is one of the ScheduledRefreshOutcome* constants.
func (r *Registry) RecordScheduledRefreshOutcome(outcome string) {
	r.scheduledRefreshOutcomes.WithLabelValues(outcome).Inc()
}

// RegisterConnectionsByStatusGauge wires the connections-by-status metrics
// gauge (PD38d, AC: "/metrics exposes a connections-by-status gauge
// (INITIATED/ACTIVE/EXPIRED/DISCONNECTED counts)"): one GaugeFunc per known
// status, each evaluated at scrape time by calling countByStatus — wiring
// passes (*connections.Facade).CountByStatus (architecture doc, Slice 7) —
// rather than maintained as a running counter, so the value is always
// exactly correct, never drifted. countByStatus is a plain function value,
// not a typed port, because metrics imports no domain module (BOUNDARIES).
func (r *Registry) RegisterConnectionsByStatusGauge(countByStatus func(ctx context.Context) (map[string]int, error)) {
	for _, status := range connectionStatuses {
		status := status
		gauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "beecon_connections_by_status",
			Help:        "Number of connections currently in each lifecycle status.",
			ConstLabels: prometheus.Labels{"status": status},
		}, func() float64 {
			counts, err := countByStatus(context.Background())
			if err != nil {
				return 0
			}
			return float64(counts[status])
		})
		r.registry.MustRegister(gauge)
	}
}

// RegisterOutboxGauges wires the outbox depth and oldest-pending-event-age
// metrics gauges (PD38d): pendingDepth and oldestPendingAge are
// (*delivery.Facade).OutboxPendingDepth/OutboxOldestPendingAge (architecture
// doc, Slice 7), each evaluated at scrape time — a query, not a maintained
// counter, so the value is always exactly correct.
func (r *Registry) RegisterOutboxGauges(
	pendingDepth func(ctx context.Context) (int, error),
	oldestPendingAge func(ctx context.Context) (time.Duration, error),
) {
	depthGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "beecon_outbox_pending_depth",
		Help: "Number of outbox events currently PENDING delivery.",
	}, func() float64 {
		depth, err := pendingDepth(context.Background())
		if err != nil {
			return 0
		}
		return float64(depth)
	})
	oldestAgeGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "beecon_outbox_oldest_pending_age_seconds",
		Help: "Age in seconds of the oldest PENDING outbox event (0 when none are pending).",
	}, func() float64 {
		age, err := oldestPendingAge(context.Background())
		if err != nil {
			return 0
		}
		return age.Seconds()
	})
	r.registry.MustRegister(depthGauge, oldestAgeGauge)
}
