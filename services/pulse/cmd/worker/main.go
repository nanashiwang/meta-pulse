package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/forum"
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
	accessCtx, accessCancel := context.WithTimeout(context.Background(), 30*time.Second)
	accessReport, accessErr := verifyLogReaderAccess(accessCtx, logReader)
	accessCancel()
	if accessErr != nil {
		logger.Error("verify new-api log reader permissions", "error", accessErr, "current_user", accessReport.CurrentUser, "database", accessReport.Database, "grant_count", accessReport.GrantCount)
		os.Exit(1)
	}
	logger.Info("verified new-api log reader permissions", "current_user", accessReport.CurrentUser, "database", accessReport.Database, "grant_count", accessReport.GrantCount)
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

	// Forum ingestion is deliberately optional and isolated. A missing or
	// unavailable forum database disables only content candidates; usage
	// ingestion, settlement, period close, and metrics keep running.
	var contentIngest *service.ContentIngestService
	var forumReader *forum.Reader
	if cfg.ForumDBDSN != "" {
		forumReader, err = forum.OpenReader(cfg.ForumDBDSN)
		if err != nil {
			logger.Warn("content ingest disabled: forum database unavailable", "error", err)
		} else {
			contentIngest, err = service.NewContentIngestService(unit, forumReader, service.ContentIngestConfig{BatchSize: cfg.ContentIngestBatchSize})
			if err != nil {
				logger.Error("initialize content ingest service", "error", err)
				_ = forumReader.Close()
				forumReader = nil
			}
		}
	}
	if forumReader != nil {
		defer forumReader.Close()
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

		usageResult, usageErr := ingest.IngestBatch(checkCtx)
		if usageErr != nil {
			logger.Warn("usage ingest failed", "error", usageErr, "fetched", usageResult.Fetched)
		} else if usageResult.Fetched > 0 {
			logger.Info("usage ingest batch completed", "fetched", usageResult.Fetched, "accepted", usageResult.Accepted, "replayed", usageResult.Replayed, "conflicts", usageResult.Conflicts, "manual_review", usageResult.ManualReview)
		}

		// Core settlement is independent of usage and forum reads. A failed
		// source must not prevent already persisted outbox records from being
		// reconciled.
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

		if contentIngest != nil {
			contentResult, contentErr := contentIngest.IngestBatch(checkCtx)
			if contentErr != nil {
				// Content is a sidecar projection; never return or panic here.
				logger.Warn("content ingest failed", "error", contentErr, "fetched", contentResult.Fetched)
			} else if contentResult.Fetched > 0 {
				logger.Info("content ingest batch completed", "fetched", contentResult.Fetched, "accepted", contentResult.Accepted, "replayed", contentResult.Replayed, "conflicts", contentResult.Conflicts)
			}
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
