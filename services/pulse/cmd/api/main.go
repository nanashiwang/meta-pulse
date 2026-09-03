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

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/health"
	"github.com/nanashiwang/meta-pulse/internal/observability"
	"github.com/nanashiwang/meta-pulse/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	redisstore "github.com/nanashiwang/meta-pulse/internal/store/redis"
	transporthttp "github.com/nanashiwang/meta-pulse/internal/transport/http"
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
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		logger.Error("initialize unit of work", "error", err)
		os.Exit(1)
	}
	profile, err := service.NewProfileService(unit, []level.Definition{
		{Key: "new", Name: "新用户", MinContributionMilli: 0},
		{Key: "pulse", Name: "脉冲者", MinContributionMilli: 1000000},
	})
	if err != nil {
		logger.Error("initialize profile service", "error", err)
		os.Exit(1)
	}
	nonces, err := cache.NewNonceStore("pulse:api:nonce")
	if err != nil {
		logger.Error("initialize request nonce store", "error", err)
		os.Exit(1)
	}
	action, err := service.NewActionService(unit, service.ActionConfig{RandomSecret: []byte(cfg.RewardRandomSecret), ShadowMode: cfg.RewardShadowMode})
	if err != nil {
		logger.Error("initialize action service", "error", err)
		os.Exit(1)
	}

	// Content awards share the normal Grant/Settlement path. The API only
	// needs the settlement dependency for reversal; an empty new-api URL keeps
	// local shadow-mode development usable without making Pulse depend on it
	// for startup or for the read-only/admin route.
	var rollback service.GrantRollbacker
	if cfg.NewAPIInternalURL != "" && cfg.ServiceHMACSecret != "" {
		benefitClient, clientErr := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
		if clientErr != nil {
			logger.Error("initialize content benefit client", "error", clientErr)
			os.Exit(1)
		}
		settlement, settlementErr := service.NewSettlementService(unit, benefitClient, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
		if settlementErr != nil {
			logger.Error("initialize content settlement service", "error", settlementErr)
			os.Exit(1)
		}
		rollback = settlement
	}
	content, err := service.NewContentAwardService(unit, service.ContentAwardConfig{
		MinPaidContributionMilli: cfg.ContentMinPaidContribution,
		MaxUserPeriodAmount:      cfg.ContentMaxUserPeriodAmount,
		MaxDailyAmount:           cfg.ContentMaxDailyAmount,
		ShadowMode:               cfg.RewardShadowMode,
	}, rollback)
	if err != nil {
		logger.Error("initialize content award service", "error", err)
		os.Exit(1)
	}
	rewardHistory, err := service.NewRewardHistoryService(unit)
	if err != nil {
		logger.Error("initialize reward history service", "error", err)
		os.Exit(1)
	}

	profileAuth := transporthttp.SignedRequestWithSecrets(func(role string) [][]byte {
		switch role {
		case "new-api":
			return cfg.UserBFFHMACSecrets()
		case "forum", "worker", "service":
			return cfg.ServiceHMACSecrets()
		case "admin":
			return cfg.AdminHMACSecrets()
		default:
			return nil
		}
	}, nonces, 5*time.Minute)
	metrics := observability.NewMetrics()
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.NewRouterWithProfileSummaryActionContentAndHistory(logger, readiness, profile, profile, action, content, rewardHistory, profileAuth, metrics),
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
