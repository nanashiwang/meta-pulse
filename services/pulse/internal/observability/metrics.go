// Package observability owns process metrics. Business facts remain in MySQL;
// metrics are diagnostics and may be recreated at any time.
package observability

import (
	"math"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type Metrics struct {
	Registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec

	IngestLagSeconds      prometheus.Gauge
	OpenConflicts         prometheus.Gauge
	LedgerMismatches      prometheus.Gauge
	SettlementRetries     prometheus.Gauge
	SettlementDead        prometheus.Gauge
	BudgetReservedAmount  prometheus.Gauge
	BudgetHardCap         prometheus.Gauge
	PeriodCloseFailures   prometheus.Counter
	OperationsUp          prometheus.Gauge
	OperationsLastSuccess prometheus.Gauge
	OperationsFailures    prometheus.Counter
	JobFailures           *prometheus.CounterVec
}

// NewHTTPMetrics avoids publishing unpopulated business gauges from API replicas.
func NewHTTPMetrics() *Metrics {
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

// NewMetrics is owned by the worker that actually computes operations snapshots.
func NewMetrics() *Metrics {
	metrics := NewHTTPMetrics()
	registry, httpRequests := metrics.Registry, metrics.HTTPRequests
	inGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "ingest_lag_seconds", Help: "Age of the last durable usage ingest watermark."})
	conflictGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "open_conflicts", Help: "Open usage ingest conflicts."})
	mismatchGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "ledger_mismatches", Help: "Accounts that do not rebuild from their ledger."})
	retryGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "settlement", Name: "retry", Help: "Settlement outboxes waiting for retry."})
	deadGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "settlement", Name: "dead", Help: "Settlement outboxes that exhausted retries or reached a terminal conflict."})
	reservedGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "budget", Name: "reserved_amount", Help: "Currently reserved reward amount."})
	hardCapGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "budget", Name: "hard_cap", Help: "Configured reward hard cap."})
	periodCloseFailures := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "meta_pulse", Subsystem: "period", Name: "close_failures_total", Help: "Period close attempts that failed."})
	up := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "up", Help: "Whether the most recent snapshot collection succeeded (0 until the first success)."})
	lastSuccess := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "last_success_timestamp_seconds", Help: "Unix timestamp of the last successfully committed operations snapshot; alert when stale."})
	failures := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "collection_failures_total", Help: "Failed operations snapshot collections."})
	jobFailures := prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "meta_pulse", Subsystem: "worker", Name: "job_failures_total", Help: "Failed background task invocations."}, []string{"task"})
	registry.MustRegister(inGauge, conflictGauge, mismatchGauge, retryGauge, deadGauge, reservedGauge, hardCapGauge, periodCloseFailures, up, lastSuccess, failures, jobFailures)
	// Unknown is not zero: startup or missing data must never look healthy.
	for _, gauge := range []prometheus.Gauge{inGauge, conflictGauge, mismatchGauge, retryGauge, deadGauge, reservedGauge, hardCapGauge} {
		gauge.Set(math.NaN())
	}
	return &Metrics{
		Registry: registry, HTTPRequests: httpRequests,
		IngestLagSeconds: inGauge, OpenConflicts: conflictGauge, LedgerMismatches: mismatchGauge,
		SettlementRetries: retryGauge, SettlementDead: deadGauge, BudgetReservedAmount: reservedGauge,
		BudgetHardCap: hardCapGauge, PeriodCloseFailures: periodCloseFailures,
		OperationsUp: up, OperationsLastSuccess: lastSuccess, OperationsFailures: failures, JobFailures: jobFailures,
	}
}

func (m *Metrics) RecordOperations(snapshot ports.OperationalSnapshot) {
	if m == nil || m.IngestLagSeconds == nil {
		return
	}
	m.IngestLagSeconds.Set(float64(snapshot.IngestLagSeconds))
	m.OpenConflicts.Set(float64(snapshot.OpenConflictCount))
	m.LedgerMismatches.Set(float64(snapshot.LedgerMismatchCount))
	m.SettlementRetries.Set(float64(snapshot.SettlementRetryCount))
	m.SettlementDead.Set(float64(snapshot.SettlementDeadCount))
	m.BudgetReservedAmount.Set(float64(snapshot.BudgetReservedAmount))
	m.BudgetHardCap.Set(float64(snapshot.BudgetHardCap))
	m.OperationsLastSuccess.Set(float64(time.Now().Unix()))
	m.OperationsUp.Set(1)
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

// Preserve the last values/timestamp, but explicitly expose collection failure.
func (m *Metrics) RecordOperationsFailure() {
	if m == nil || m.OperationsUp == nil {
		return
	}
	m.OperationsUp.Set(0)
	m.OperationsFailures.Inc()
}
