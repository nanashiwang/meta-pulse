package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
)

type RuntimeConfigProvider interface {
	Current(context.Context) (config.Config, error)
}

type ReadinessFunc func(context.Context) error

func (f ReadinessFunc) Check(ctx context.Context) error { return f(ctx) }

// RuntimeHandler resolves one immutable configuration for each request. An
// unavailable store never falls back to an old enabled switch or revoked key.
// Routers are rebuilt only when the effective API configuration changes;
// requests already in flight retain their original snapshot.
func RuntimeHandler(provider RuntimeConfigProvider, build func(config.Config) (http.Handler, error), diagnostics http.Handler) http.Handler {
	var mu sync.Mutex
	var previous config.Config
	var current http.Handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			diagnostics.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		cfg, err := provider.Current(ctx)
		cancel()
		if err == nil {
			err = cfg.ValidateAPI()
		}
		if err != nil {
			runtimeUnavailable(w)
			return
		}
		mu.Lock()
		if current == nil || previous != cfg {
			var next http.Handler
			next, err = build(cfg)
			if err == nil {
				current, previous = next, cfg
			}
		}
		handler := current
		mu.Unlock()
		if err != nil || handler == nil {
			runtimeUnavailable(w)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func runtimeUnavailable(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"settings_unavailable"}`))
}
