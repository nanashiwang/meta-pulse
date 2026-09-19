package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/forum"
	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/buildinfo"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/health"
	"github.com/nanashiwang/meta-pulse/internal/job"
	"github.com/nanashiwang/meta-pulse/internal/observability"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
	"github.com/nanashiwang/meta-pulse/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	redisstore "github.com/nanashiwang/meta-pulse/internal/store/redis"
)

func main() {
	if buildinfo.PrintVersion() {
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		logger.Error("initialize pulse database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	runtime, err := runtimeconfig.New(initCtx, mysqlstore.NewRuntimeConfigStore(database), runtimeconfig.RoleWorker, cfg.RuntimeKeyDir, cfg)
	if err == nil {
		cfg, err = runtime.Current(initCtx)
	}
	initCancel()
	if err == nil {
		err = cfg.ValidateWorker()
	}
	if err != nil {
		logger.Error("initialize worker runtime configuration", "error", err)
		os.Exit(1)
	}

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
	ingest, err := service.NewUsageIngestService(unit, source, service.UsageIngestConfig{BatchSize: cfg.IngestBatchSize, SourceSystem: "new-api-log", TicketThresholdMilli: cfg.TicketThresholdMilli, BatchTimeBudget: 15 * time.Second})
	if err != nil {
		logger.Error("initialize usage ingest", "error", err)
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

	dependencies := health.NewChecker(map[string]health.Pinger{"mysql": database, "redis": cache})
	readiness := app.ReadinessFunc(func(checkCtx context.Context) error {
		if err := dependencies.Check(checkCtx); err != nil {
			return err
		}
		current, err := runtime.Current(checkCtx)
		if err != nil {
			return err
		}
		return current.ValidateWorker()
	})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	metrics := observability.NewMetrics()
	listener, err := net.Listen("tcp", cfg.WorkerHTTPAddr)
	if err != nil {
		logger.Error("listen on worker diagnostics address", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Handler: app.NewWorkerHandler(readiness, metrics), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()

	// Each task has its own non-overlapping loop and root-derived deadline.
	// Slow LOG_DB, forum or metrics queries cannot consume settlement's timeout.
	// Tasks that call an external database also back off on consecutive
	// failures, so a dependency that is timing out is not hammered at a fixed
	// interval until it recovers.
	var tasks []job.Task
	addTask := func(name string, run func(context.Context) error) {
		tasks = append(tasks, job.Task{Name: name, Interval: 30 * time.Second, Timeout: 20 * time.Second, Run: run})
	}
	addExternalTask := func(name string, run func(context.Context) error) {
		tasks = append(tasks, job.Task{Name: name, Interval: 30 * time.Second, Timeout: 20 * time.Second, MaxBackoff: 10 * time.Minute, Run: run})
	}

	addExternalTask("usage_ingest", func(checkCtx context.Context) error {
		startedAt := time.Now()
		usageResult, usageErr := ingest.IngestBatch(checkCtx)
		if usageErr != nil {
			logger.Warn("usage ingest failed", "error", usageErr, "fetched", usageResult.Fetched, "accepted", usageResult.Accepted, "elapsed_ms", time.Since(startedAt).Milliseconds())
		} else if usageResult.Fetched > 0 {
			logger.Info("usage ingest batch completed", "fetched", usageResult.Fetched, "accepted", usageResult.Accepted, "replayed", usageResult.Replayed, "conflicts", usageResult.Conflicts, "manual_review", usageResult.ManualReview, "yielded", usageResult.Yielded, "elapsed_ms", time.Since(startedAt).Milliseconds())
		}

		return usageErr
	})

	addTask("settlement", func(checkCtx context.Context) error {
		settlement, err := newRuntimeSettlement(checkCtx, runtime, unit)
		if err != nil {
			return err
		}
		settlementResult, settlementErr := settlement.ProcessBatch(checkCtx)
		if settlementErr != nil {
			logger.Warn("settlement batch failed", "error", settlementErr)
		} else if settlementResult.Claimed > 0 {
			logger.Info("settlement batch completed", "claimed", settlementResult.Claimed, "completed", settlementResult.Completed, "retried", settlementResult.Retried, "dead", settlementResult.Dead, "failed", settlementResult.Failed)
		}

		return settlementErr
	})

	addTask("reconciliation", func(checkCtx context.Context) error {
		settlement, err := newRuntimeSettlement(checkCtx, runtime, unit)
		if err != nil {
			return err
		}
		reconciliation, reconciliationErr := settlement.Reconcile(checkCtx)
		if reconciliationErr != nil {
			logger.Warn("settlement reconciliation failed", "error", reconciliationErr)
		} else if reconciliation.Settled > 0 {
			logger.Info("settlement reconciliation completed", "checked", reconciliation.Checked, "settled", reconciliation.Settled, "unchanged", reconciliation.Unchanged, "failed", reconciliation.Failed)
		}

		return reconciliationErr
	})

	addTask("period_close", func(checkCtx context.Context) error {
		periodCloser, err := newRuntimePeriodCloser(checkCtx, runtime, unit)
		if err != nil {
			return err
		}
		periodReport, periodErr := periodCloser.RunOnce(checkCtx)
		if periodErr != nil {
			logger.Warn("period close failed", "error", periodErr)
		} else if periodReport.Checked > 0 {
			logger.Info("period close completed", "checked", periodReport.Checked, "closed", periodReport.Closed, "deferred", periodReport.Deferred, "failed", periodReport.Failed, "tickets_expired", periodReport.TicketsExpired, "rewards_created", periodReport.RewardsCreated)
		}

		if periodReport.Failed > 0 {
			metrics.PeriodCloseFailures.Add(float64(periodReport.Failed))
		}
		if periodErr != nil && periodReport.Failed == 0 {
			metrics.PeriodCloseFailures.Inc()
		}
		if periodErr == nil && periodReport.Failed > 0 {
			return fmt.Errorf("%d periods failed to close", periodReport.Failed)
		}

		return periodErr
	})

	addTask("operations", func(checkCtx context.Context) error {
		metricsSnapshot, metricsErr := metricsAggregation.Aggregate(checkCtx)
		if metricsErr != nil {
			logger.Warn("operational metrics aggregation failed", "error", metricsErr)
		} else if metricsSnapshot.LedgerMismatchCount > 0 || metricsSnapshot.SettlementDeadCount > 0 {
			logger.Warn("pulse operational alert", "ledger_mismatches", metricsSnapshot.LedgerMismatchCount, "settlement_dead", metricsSnapshot.SettlementDeadCount, "open_conflicts", metricsSnapshot.OpenConflictCount)
		}

		if metricsErr != nil {
			metrics.RecordOperationsFailure()
		} else {
			metrics.RecordOperations(metricsSnapshot)
		}

		return metricsErr
	})

	if contentIngest != nil {
		addExternalTask("content_ingest", func(checkCtx context.Context) error {
			contentResult, contentErr := contentIngest.IngestBatch(checkCtx)
			if contentErr != nil {
				// Content is a sidecar projection; never return or panic here.
				logger.Warn("content ingest failed", "error", contentErr, "fetched", contentResult.Fetched)
			} else if contentResult.Fetched > 0 {
				logger.Info("content ingest batch completed", "fetched", contentResult.Fetched, "accepted", contentResult.Accepted, "replayed", contentResult.Replayed, "conflicts", contentResult.Conflicts)
			}
			return contentErr
		})
	}
	tasks = append(tasks, job.Task{Name: "dependencies", Interval: 30 * time.Second, Timeout: 2 * time.Second, Run: readiness.Check})
	jobsDone := make(chan struct{})
	go func() {
		defer close(jobsDone)
		if err := job.Run(ctx, tasks, func(name string, err error) {
			if err != nil {
				metrics.JobFailures.WithLabelValues(name).Inc()
				logger.Warn("worker job failed", "job", name, "error", err)
			}
		}); err != nil {
			logger.Error("worker scheduler failed", "error", err)
			stop()
		}
	}()
	logger.Info("meta-pulse-worker started", "environment", cfg.Environment, "diagnostics_addr", cfg.WorkerHTTPAddr)
	var serverErr error
	select {
	case <-ctx.Done():
	case serverErr = <-serverErrors:
		if !errors.Is(serverErr, http.ErrServerClosed) {
			logger.Error("worker diagnostics stopped", "error", serverErr)
		}
	}
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("worker diagnostics shutdown failed", "error", err)
		_ = server.Close()
	}
	<-jobsDone
	logger.Info("meta-pulse-worker stopped")
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		os.Exit(1)
	}
}
