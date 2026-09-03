package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/health"
	"github.com/nanashiwang/meta-pulse/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	redisstore "github.com/nanashiwang/meta-pulse/internal/store/redis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if err := cfg.ValidateWorker(); err != nil {
		logger.Error("invalid worker configuration", "error", err)
		os.Exit(1)
	}

	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		logger.Error("initialize pulse database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	cache, err := redisstore.Open(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		logger.Error("initialize redis", "error", err)
		os.Exit(1)
	}
	defer cache.Close()
	logReader, err := newapi.OpenLogReader(cfg.NewAPILogDSN)
	if err != nil {
		logger.Error("initialize new-api log reader", "error", err)
		os.Exit(1)
	}
	defer logReader.Close()
	source, err := newapi.NewLogSource(logReader, "new-api-log")
	if err != nil {
		logger.Error("initialize new-api usage source", "error", err)
		os.Exit(1)
	}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		logger.Error("initialize unit of work", "error", err)
		os.Exit(1)
	}
	ingest, err := service.NewUsageIngestService(unit, source, service.UsageIngestConfig{BatchSize: cfg.IngestBatchSize, SourceSystem: "new-api-log", TicketThresholdMilli: cfg.TicketThresholdMilli})
	if err != nil {
		logger.Error("initialize usage ingest", "error", err)
		os.Exit(1)
	}
	benefitClient, err := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
	if err != nil {
		logger.Error("initialize benefit client", "error", err)
		os.Exit(1)
	}
	settlement, err := service.NewSettlementService(unit, benefitClient, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
	if err != nil {
		logger.Error("initialize settlement service", "error", err)
		os.Exit(1)
	}
	periodCloser, err := service.NewPeriodCloseService(unit, service.PeriodCloseConfig{
		BatchSize: cfg.PeriodCloseBatchSize, CursorName: service.DefaultUsageCursorName,
		SourceSystem: "new-api-log", RequireWatermark: cfg.PeriodCloseRequireWatermark,
		EnablePeriodRewards: cfg.PeriodRewardsEnabled, RandomSecret: []byte(cfg.RewardRandomSecret),
		ShadowMode: cfg.RewardShadowMode,
	})
	if err != nil {
		logger.Error("initialize period close service", "error", err)
		os.Exit(1)
	}
	metricsAggregation, err := service.NewMetricsAggregationService(unit, service.MetricsAggregationConfig{
		CursorName: service.DefaultUsageCursorName, SourceSystem: "new-api-log",
	})
	if err != nil {
		logger.Error("initialize metrics aggregation service", "error", err)
		os.Exit(1)
	}

	readiness := health.NewChecker(map[string]health.Pinger{"mysql": database, "redis": cache})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("meta-pulse-worker started", "environment", cfg.Environment)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	runBatch := func() {
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		result, err := ingest.IngestBatch(checkCtx)
		if err != nil {
			logger.Warn("usage ingest failed", "error", err, "fetched", result.Fetched)
			return
		}
		if result.Fetched > 0 {
			logger.Info("usage ingest batch completed", "fetched", result.Fetched, "accepted", result.Accepted, "replayed", result.Replayed, "conflicts", result.Conflicts, "manual_review", result.ManualReview)
		}
		settlementResult, settlementErr := settlement.ProcessBatch(checkCtx)
		if settlementErr != nil {
			logger.Warn("settlement batch failed", "error", settlementErr)
		} else if settlementResult.Claimed > 0 {
			logger.Info("settlement batch completed", "claimed", settlementResult.Claimed, "completed", settlementResult.Completed, "retried", settlementResult.Retried, "dead", settlementResult.Dead, "failed", settlementResult.Failed)
		}
		reconciliation, reconciliationErr := settlement.Reconcile(checkCtx)
		if reconciliationErr != nil {
			logger.Warn("settlement reconciliation failed", "error", reconciliationErr)
		} else if reconciliation.Settled > 0 {
			logger.Info("settlement reconciliation completed", "checked", reconciliation.Checked, "settled", reconciliation.Settled, "unchanged", reconciliation.Unchanged, "failed", reconciliation.Failed)
		}
		periodReport, periodErr := periodCloser.RunOnce(checkCtx)
		if periodErr != nil {
			logger.Warn("period close failed", "error", periodErr)
		} else if periodReport.Checked > 0 {
			logger.Info("period close completed", "checked", periodReport.Checked, "closed", periodReport.Closed, "deferred", periodReport.Deferred, "failed", periodReport.Failed, "tickets_expired", periodReport.TicketsExpired, "rewards_created", periodReport.RewardsCreated)
		}
		metricsSnapshot, metricsErr := metricsAggregation.Aggregate(checkCtx)
		if metricsErr != nil {
			logger.Warn("operational metrics aggregation failed", "error", metricsErr)
		} else if metricsSnapshot.LedgerMismatchCount > 0 || metricsSnapshot.SettlementDeadCount > 0 {
			logger.Warn("pulse operational alert", "ledger_mismatches", metricsSnapshot.LedgerMismatchCount, "settlement_dead", metricsSnapshot.SettlementDeadCount, "open_conflicts", metricsSnapshot.OpenConflictCount)
		}
	}

	// Run immediately on startup, then continue polling. No new-api relay
	// request waits for this worker.
	runBatch()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := readiness.Check(checkCtx)
		cancel()
		if err != nil {
			logger.Warn("worker dependencies not ready", "error", err)
		} else {
			runBatch()
		}

		select {
		case <-ctx.Done():
			logger.Info("meta-pulse-worker stopped")
			return
		case <-ticker.C:
		}
	}
}
