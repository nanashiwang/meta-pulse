// Command metar-intel-api serves the anonymous, read-only model leaderboard.
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

	"github.com/nanashiwang/meta-pulse/services/intel/internal/admin"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/config"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/settings"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
	transporthttp "github.com/nanashiwang/meta-pulse/services/intel/internal/transport/http"
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

	reader := service.NewReader(store, score.V1, cfg.CacheTTL)
	router := transporthttp.NewRouter(logger, reader, store, cfg.SpeedWindow)
	adminSvc := &admin.Service{Store: store, Method: score.V1, SpeedWindow: cfg.SpeedWindow}
	var box *settings.Box
	if len(cfg.AdminSecrets) > 0 {
		box, _ = settings.NewBox(cfg.AdminSecrets[0])
	}
	sources := &admin.Sources{Store: store, Box: box, EnvKey: cfg.LLMStatsAPIKey, BaseURL: cfg.LLMStatsBaseURL,
		SyncInterval: cfg.LLMStatsSyncInterval, Verify: admin.VerifyWithAPI}
	if transporthttp.RegisterAdmin(router, logger, adminSvc, sources, cfg.AdminSecrets, store, time.Now) {
		logger.Info("intel admin API enabled")
	} else {
		logger.Info("intel admin API disabled: INTEL_ADMIN_HMAC_SECRET not set")
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() {
		logger.Info("metar-intel-api started", "addr", cfg.HTTPAddr, "environment", cfg.Environment)
		errs <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metar-intel-api stopped unexpectedly", "error", err)
			os.Exit(1)
		}
		return
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
}
