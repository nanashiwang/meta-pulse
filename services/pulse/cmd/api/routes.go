package main

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/observability"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	transporthttp "github.com/nanashiwang/meta-pulse/internal/transport/http"
)

func buildAPIRouter(cfg config.Config, logger *slog.Logger, readiness app.ReadinessChecker, unit ports.UnitOfWork, nonces security.NonceStore, settings transporthttp.RuntimeSettings, metrics *observability.Metrics) (http.Handler, error) {
	profile, err := service.NewProfileService(unit, []level.Definition{
		{Key: "new", Name: "新用户", MinContributionMilli: 0},
		{Key: "pulse", Name: "脉冲者", MinContributionMilli: 1000000},
	})
	if err != nil {
		return nil, err
	}
	action, err := service.NewActionService(unit, service.ActionConfig{RandomSecret: []byte(cfg.RewardRandomSecret), ShadowMode: cfg.RewardShadowMode, DisableNewActions: !cfg.ActionsEnabled || cfg.RewardShadowMode, RequireVerifiedFunding: true})
	if err != nil {
		return nil, err
	}
	var rollback service.GrantRollbacker
	if cfg.NewAPIInternalURL != "" && cfg.RollbackHMACSecret != "" {
		client, err := newapi.NewRollbackClient(cfg.NewAPIInternalURL, []byte(cfg.RollbackHMACSecret), nil)
		if err != nil {
			return nil, err
		}
		rollback, err = service.NewSettlementService(unit, client, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
		if err != nil {
			return nil, err
		}
	}
	content, err := service.NewContentAwardService(unit, service.ContentAwardConfig{
		MinPaidContributionMilli: cfg.ContentMinPaidContribution,
		MaxUserPeriodAmount:      cfg.ContentMaxUserPeriodAmount, MaxDailyAmount: cfg.ContentMaxDailyAmount,
		ShadowMode: cfg.RewardShadowMode,
	}, rollback)
	if err != nil {
		return nil, err
	}
	history, err := service.NewRewardHistoryService(unit)
	if err != nil {
		return nil, err
	}
	operations, err := service.NewOperationsOverviewService(unit, service.OperationsOverviewConfig{CursorName: service.DefaultUsageCursorName, SourceSystem: "new-api-log"}, time.Now)
	if err != nil {
		return nil, err
	}
	periods, err := service.NewPeriodCreateService(unit, time.Now)
	if err != nil {
		return nil, err
	}
	auth := transporthttp.SignedRequestWithSecrets(func(role string) [][]byte {
		switch role {
		case "community-bff":
			return cfg.CommunityBFFHMACSecrets()
		case "new-api":
			return cfg.UserBFFHMACSecrets()
		case "forum":
			return cfg.ForumHMACSecrets()
		case "admin":
			return cfg.AdminHMACSecrets()
		default:
			return nil
		}
	}, nonces, 5*time.Minute)
	rules := service.NewRewardRulesService(unit, cfg.ActionsEnabled && !cfg.RewardShadowMode)
	rules.QuotaPerUnit = cfg.QuotaPerUnit
	return app.NewRouterWithRoutes(logger, readiness, app.APIRoutes{
		Profile: profile, Summary: profile, Action: action, Content: content,
		Experience: service.NewExperienceService(unit), History: history, Rules: rules, Operations: operations, Settings: settings, Periods: periods, Auth: auth,
	}, metrics), nil
}
