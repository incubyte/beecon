// Package metrics owns Beecon's Prometheus registry (PD24): the operability
// signals the reviewer asked for before production dependence — tool
// execution counts and durations by provider and status, upstream rate-limit
// retries, OAuth handshake outcomes, and token-refresh outcomes. It is
// shared infrastructure (BOUNDARIES: importable by any module, imports no
// domain module), like idgen, httpx, or vault — injected concretely into the
// execution and connections facades rather than behind a port, since there
// is exactly one implementation (YAGNI, architecture Flagged Decision 3).
package metrics

import (
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
}

// New builds a Registry carrying only Beecon's own metrics — a dedicated
// prometheus.Registry rather than the global default, so GET /metrics
// exposes exactly PD24's signals.
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
	}
	r.registry.MustRegister(
		r.toolExecutions,
		r.toolExecutionDuration,
		r.rateLimitRetries,
		r.oauthHandshakes,
		r.tokenRefreshes,
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
