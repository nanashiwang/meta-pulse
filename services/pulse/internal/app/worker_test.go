package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/observability"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type workerReadiness struct{ err error }

func (r workerReadiness) Check(context.Context) error { return r.err }

func TestWorkerDiagnosticsExposeTheCollectedSnapshot(t *testing.T) {
	metrics := observability.NewMetrics()
	metrics.RecordOperations(ports.OperationalSnapshot{SettlementDeadCount: 3})
	for _, depErr := range []error{nil, errors.New("database down")} {
		handler := NewWorkerHandler(workerReadiness{depErr}, metrics)
		for path, want := range map[string]int{"/healthz": 200, "/metrics": 200, "/readyz": 200} {
			if path == "/readyz" && depErr != nil {
				want = 503
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code != want {
				t.Fatalf("%s status=%d want=%d", path, response.Code, want)
			}
			if path == "/metrics" && !strings.Contains(response.Body.String(), "meta_pulse_settlement_dead 3\n") {
				t.Fatal("worker did not expose actual snapshot")
			}
		}
	}
}

func TestRequestMetricsUseBoundedRouteLabels(t *testing.T) {
	metrics := observability.NewHTTPMetrics()
	router := gin.New()
	router.Use(requestMetrics(metrics))
	router.GET("/users/:id", func(c *gin.Context) { c.Status(200) })
	for _, path := range []string{"/users/1", "/users/2", "/missing/1", "/missing/2"} {
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	if !strings.Contains(body, `path="/users/:id",status="200"} 2`) || !strings.Contains(body, `path="unmatched",status="404"} 2`) {
		t.Fatalf("unexpected metric labels: %s", body)
	}
}
