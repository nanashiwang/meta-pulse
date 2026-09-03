// Package observability owns process metrics. Business facts remain in MySQL;
// metrics are diagnostics and may be recreated at any time.
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	httpRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "meta_pulse",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests handled by the Pulse API.",
	}, []string{"method", "path", "status"})
	registry.MustRegister(httpRequests)
	return &Metrics{Registry: registry, HTTPRequests: httpRequests}
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
