package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/health"
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

	readiness := health.NewChecker(map[string]health.Pinger{"mysql": database, "redis": cache})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("meta-pulse-worker started", "environment", cfg.Environment)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := readiness.Check(checkCtx)
		cancel()
		if err != nil {
			logger.Warn("worker dependencies not ready", "error", err)
		} else {
			logger.Info("worker heartbeat", "status", "ready")
		}

		select {
		case <-ctx.Done():
			logger.Info("meta-pulse-worker stopped")
			return
		case <-ticker.C:
		}
	}
}
