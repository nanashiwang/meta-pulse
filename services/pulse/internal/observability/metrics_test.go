package observability

import (
	"github.com/nanashiwang/meta-pulse/internal/ports"
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

func TestOperationsMetricsExposeValuesFreshnessAndFailure(t *testing.T) {
	metrics := NewMetrics()
	scrape := func() string {
		t.Helper()
		response := httptest.NewRecorder()
		metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
		if response.Code != 200 {
			t.Fatalf("scrape status=%d", response.Code)
		}
		return response.Body.String()
	}
	assertContains := func(body string, lines ...string) {
		t.Helper()
		for _, line := range lines {
			if !strings.Contains(body, line+"\n") {
				t.Fatalf("missing %q in scrape", line)
			}
		}
	}
	assertContains(scrape(), "meta_pulse_operations_up 0", "meta_pulse_operations_ledger_mismatches NaN", "meta_pulse_operations_last_success_timestamp_seconds 0")
	metrics.RecordOperations(ports.OperationalSnapshot{IngestLagSeconds: 37, OpenConflictCount: 2, LedgerMismatchCount: 3, SettlementRetryCount: 4, SettlementDeadCount: 5, BudgetReservedAmount: 70, BudgetHardCap: 100})
	metrics.PeriodCloseFailures.Inc()
	metrics.JobFailures.WithLabelValues("period_close").Inc()
	assertContains(scrape(), "meta_pulse_operations_up 1", "meta_pulse_operations_ingest_lag_seconds 37", "meta_pulse_operations_open_conflicts 2", "meta_pulse_operations_ledger_mismatches 3", "meta_pulse_settlement_retry 4", "meta_pulse_settlement_dead 5", "meta_pulse_budget_reserved_amount 70", "meta_pulse_budget_hard_cap 100", "meta_pulse_period_close_failures_total 1", "meta_pulse_worker_job_failures_total{task=\"period_close\"} 1")
	if strings.Contains(scrape(), "meta_pulse_operations_last_success_timestamp_seconds 0\n") {
		t.Fatal("successful snapshot has no timestamp")
	}
	metrics.RecordOperationsFailure()
	assertContains(scrape(), "meta_pulse_operations_up 0", "meta_pulse_operations_collection_failures_total 1", "meta_pulse_settlement_dead 5")
	metrics.RecordOperations(ports.OperationalSnapshot{})
	assertContains(scrape(), "meta_pulse_operations_up 1", "meta_pulse_settlement_dead 0")
}

func TestAPIRegistryDoesNotAdvertiseUncollectedBusinessMetrics(t *testing.T) {
	metrics := NewHTTPMetrics()
	metrics.HTTPRequests.WithLabelValues("GET", "/readyz", "200").Inc()
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	if !strings.Contains(body, "meta_pulse_http_requests_total") {
		t.Fatal("missing HTTP metrics")
	}
	for _, prefix := range []string{"meta_pulse_operations_", "meta_pulse_settlement_", "meta_pulse_budget_", "meta_pulse_period_"} {
		if strings.Contains(body, prefix) {
			t.Fatalf("API publishes uncollected %s metrics", prefix)
		}
	}
}
