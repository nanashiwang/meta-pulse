// Package observability owns process metrics. Business facts remain in MySQL;
// metrics are diagnostics and may be recreated at any time.
package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type Metrics struct {
	Registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec

	IngestLagSeconds     prometheus.Gauge
	OpenConflicts        prometheus.Gauge
	LedgerMismatches     prometheus.Gauge
	SettlementRetries    prometheus.Gauge
	SettlementDead       prometheus.Gauge
	BudgetReservedAmount prometheus.Gauge
	BudgetHardCap        prometheus.Gauge
	PeriodCloseFailures  prometheus.Counter
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	httpRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "meta_pulse",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total HTTP requests handled by the Pulse API.",
	}, []string{"method", "path", "status"})
	inGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "ingest_lag_seconds", Help: "Age of the last durable usage ingest watermark."})
	conflictGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "open_conflicts", Help: "Open usage ingest conflicts."})
	mismatchGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "operations", Name: "ledger_mismatches", Help: "Accounts that do not rebuild from their ledger."})
	retryGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "settlement", Name: "retry", Help: "Settlement outboxes waiting for retry."})
	deadGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "settlement", Name: "dead", Help: "Settlement outboxes that exhausted retries."})
	reservedGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "budget", Name: "reserved_amount", Help: "Currently reserved reward amount."})
	hardCapGauge := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: "meta_pulse", Subsystem: "budget", Name: "hard_cap", Help: "Configured reward hard cap."})
	periodCloseFailures := prometheus.NewCounter(prometheus.CounterOpts{Namespace: "meta_pulse", Subsystem: "period", Name: "close_failures_total", Help: "Period close attempts that failed."})
	registry.MustRegister(httpRequests, inGauge, conflictGauge, mismatchGauge, retryGauge, deadGauge, reservedGauge, hardCapGauge, periodCloseFailures)
	return &Metrics{
		Registry: registry, HTTPRequests: httpRequests,
		IngestLagSeconds: inGauge, OpenConflicts: conflictGauge, LedgerMismatches: mismatchGauge,
		SettlementRetries: retryGauge, SettlementDead: deadGauge, BudgetReservedAmount: reservedGauge,
		BudgetHardCap: hardCapGauge, PeriodCloseFailures: periodCloseFailures,
	}
}

func (m *Metrics) RecordOperations(snapshot ports.OperationalSnapshot) {
	if m == nil {
		return
	}
	m.IngestLagSeconds.Set(float64(snapshot.IngestLagSeconds))
	m.OpenConflicts.Set(float64(snapshot.OpenConflictCount))
	m.LedgerMismatches.Set(float64(snapshot.LedgerMismatchCount))
	m.SettlementRetries.Set(float64(snapshot.SettlementRetryCount))
	m.SettlementDead.Set(float64(snapshot.SettlementDeadCount))
	m.BudgetReservedAmount.Set(float64(snapshot.BudgetReservedAmount))
	m.BudgetHardCap.Set(float64(snapshot.BudgetHardCap))
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.Registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
