// Command metar-intel-worker syncs external sources on their own interval
// and rebuilds the leaderboard snapshot on every tick.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/config"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/source/llmstats"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	store, err := mysqlstore.Open(cfg.DBDSN)
	if err != nil {
		logger.Error("open intel database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("metar-intel-worker started", "interval", cfg.ScoreInterval.String(), "llmstats_sync", cfg.LLMStatsAPIKey != "")
	ticker := time.NewTicker(cfg.ScoreInterval)
	defer ticker.Stop()
	for {
		run(ctx, logger, store, cfg)
		select {
		case <-ctx.Done():
			logger.Info("metar-intel-worker stopped")
			return
		case <-ticker.C:
		}
	}
}

// run never exits the process on failure: the previous snapshot keeps being
// served and the next tick retries.
func run(ctx context.Context, logger *slog.Logger, store *mysqlstore.Store, cfg config.Config) {
	if cfg.LLMStatsAPIKey != "" {
		syncLLMStats(ctx, logger, store, cfg)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	snap, stored, err := service.Recompute(ctx, store, score.V1, time.Now(), cfg.SpeedWindow)
	if err != nil {
		logger.Error("recompute leaderboard", "error", err)
		return
	}
	logger.Info("leaderboard recomputed", "method", snap.MethodVersion, "rows", len(snap.Rows), "stored", stored, "fingerprint", snap.Fingerprint[:12])
}

// syncLLMStats runs at most once per interval, measured from the last
// successful import, so restarts never spend extra API quota. A failed sync
// is retried on the next tick.
func syncLLMStats(ctx context.Context, logger *slog.Logger, store *mysqlstore.Store, cfg config.Config) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	last, ok, err := store.LastImportAt(ctx, llmstats.SourceSlug)
	if err != nil {
		logger.Error("read last llm-stats sync", "error", err)
		return
	}
	if ok && time.Since(last) < cfg.LLMStatsSyncInterval {
		return
	}
	client := llmstats.NewClient(cfg.LLMStatsBaseURL, cfg.LLMStatsAPIKey)
	res, err := llmstats.Sync(ctx, client, store, time.Now(), false)
	if err != nil {
		logger.Error("llm-stats sync failed", "error", err, "requests", res.Requests)
		return
	}
	logger.Info("llm-stats sync done", "requests", res.Requests, "added", res.Stats.Added, "existing", res.Stats.Skipped, "report", res.Report.String())
}
