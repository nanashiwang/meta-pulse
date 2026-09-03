package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/health"
	"github.com/nanashiwang/meta-pulse/internal/observability"
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

	readiness := health.NewChecker(map[string]health.Pinger{
		"mysql": database,
		"redis": cache,
	})
	metrics := observability.NewMetrics()
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.NewRouter(logger, readiness, metrics),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("meta-pulse-api started", "addr", cfg.HTTPAddr, "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("meta-pulse-api shutting down")
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("meta-pulse-api stopped unexpectedly", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
