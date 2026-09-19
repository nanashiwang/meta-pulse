package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
)

type runtimeFixture struct {
	mu  sync.Mutex
	cfg config.Config
	err error
}

func (f *runtimeFixture) Current(context.Context) (config.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg, f.err
}

func validRuntimeFixture() config.Config {
	return config.Config{HTTPAddr: ":8088", PulseDBDSN: "test", RedisAddr: "test", IngestBatchSize: 1, SettlementBatchSize: 1, PeriodCloseBatchSize: 1, ContentIngestBatchSize: 1, ContentMaxUserPeriodAmount: 100, ContentMaxDailyAmount: 1000, TicketThresholdMilli: 1000, RewardRandomSecret: "random", ForumHMACSecret: "forum", UserBFFHMACSecret: "user", AdminHMACSecret: "admin"}
}

func TestRuntimeHandlerRefreshesKeysAndSwitchWithoutStaleFallback(t *testing.T) {
	provider := &runtimeFixture{cfg: validRuntimeFixture()}
	builds := 0
	handler := RuntimeHandler(provider, func(cfg config.Config) (http.Handler, error) {
		builds++
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Test-Key") != cfg.UserBFFHMACSecret {
				w.WriteHeader(401)
				return
			}
			if !cfg.ActionsEnabled {
				w.WriteHeader(409)
				return
			}
			w.WriteHeader(200)
		}), nil
	}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	request := func(path, key string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Test-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d", path, w.Code, want)
		}
	}
	request("/action", "user", 409)
	request("/action", "user", 409)
	if builds != 1 {
		t.Fatalf("unchanged configuration rebuilt %d times", builds)
	}
	provider.cfg.ActionsEnabled = true
	provider.cfg.QuotaPerUnit = 500000
	provider.cfg.UserBFFHMACSecret = "rotated-user"
	request("/action", "user", 401)
	request("/action", "rotated-user", 200)
	provider.err = errors.New("database offline")
	request("/action", "rotated-user", 503)
	request("/readyz", "", 503)
	request("/healthz", "", 204)
	provider.err = nil
	provider.cfg.ActionsEnabled = false
	request("/action", "rotated-user", 409)
}

func TestRuntimeHandlerInflightUsesOneSnapshot(t *testing.T) {
	provider := &runtimeFixture{cfg: validRuntimeFixture()}
	started, release := make(chan struct{}), make(chan struct{})
	handler := RuntimeHandler(provider, func(cfg config.Config) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				close(started)
				<-release
			}
			_, _ = w.Write([]byte(cfg.UserBFFHMACSecret))
		}), nil
	}, http.NotFoundHandler())
	old := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); handler.ServeHTTP(old, httptest.NewRequest("GET", "/old", nil)) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not enter handler")
	}
	provider.mu.Lock()
	provider.cfg.UserBFFHMACSecret = "rotated"
	provider.mu.Unlock()
	newer := httptest.NewRecorder()
	handler.ServeHTTP(newer, httptest.NewRequest("GET", "/new", nil))
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not complete")
	}
	if old.Body.String() != "user" || newer.Body.String() != "rotated" {
		t.Fatal("request configuration snapshots mixed")
	}
}
