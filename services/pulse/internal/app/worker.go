package app

import (
	"context"
	"net/http"

	"github.com/nanashiwang/meta-pulse/internal/observability"
)

// NewWorkerHandler exposes diagnostics only, on the private container network.
// Readiness checks Pulse dependencies, never new-api or the forum.
func NewWorkerHandler(readiness ReadinessChecker, metrics *observability.Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"meta-pulse-worker"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if readiness == nil || readiness.Check(ctx) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not_ready","service":"meta-pulse-worker"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","service":"meta-pulse-worker"}`))
	})
	return mux
}
