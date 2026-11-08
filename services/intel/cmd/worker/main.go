// Command metar-intel-worker periodically rebuilds the leaderboard snapshot.
// External collectors will run here too; M1 data arrives through the import
// tool, so the worker only scores.
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
	logger.Info("metar-intel-worker started", "interval", cfg.ScoreInterval.String())
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
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	snap, stored, err := service.Recompute(ctx, store, score.V1, time.Now(), cfg.SpeedWindow)
	if err != nil {
		logger.Error("recompute leaderboard", "error", err)
		return
	}
	logger.Info("leaderboard recomputed", "method", snap.MethodVersion, "rows", len(snap.Rows), "stored", stored, "fingerprint", snap.Fingerprint[:12])
}
