package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsRegistryExposesRegisteredCollectors(t *testing.T) {
	metrics := NewMetrics()
	metrics.HTTPRequests.WithLabelValues("GET", "/healthz", "200").Inc()

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 200 {
		t.Fatalf("metrics status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "meta_pulse_http_requests_total") {
		t.Fatal("registered request counter not found in metrics output")
	}
}
