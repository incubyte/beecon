package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the http.Handler GET /metrics serves (PD24): Prometheus
// text format over this Registry's own registry, mounted in app/router.go
// behind AdminAuth.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}
