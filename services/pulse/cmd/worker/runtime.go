package main

import (
	"context"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/app"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

// Resolve at the start of each job, not for each grant: a job's query and send
// must use the same destination/key snapshot, including after an uncertain send.
func newRuntimeSettlement(ctx context.Context, provider app.RuntimeConfigProvider, unit ports.UnitOfWork) (*service.SettlementService, error) {
	cfg, err := provider.Current(ctx)
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateWorker(); err != nil {
		return nil, err
	}
	client, err := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
	if err != nil {
		return nil, err
	}
	return service.NewSettlementService(unit, client, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
}

func newRuntimePeriodCloser(ctx context.Context, provider app.RuntimeConfigProvider, unit ports.UnitOfWork) (*service.PeriodCloseService, error) {
	cfg, err := provider.Current(ctx)
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateWorker(); err != nil {
		return nil, err
	}
	return service.NewPeriodCloseService(unit, service.PeriodCloseConfig{
		BatchSize: cfg.PeriodCloseBatchSize, CursorName: service.DefaultUsageCursorName,
		SourceSystem: "new-api-log", RequireWatermark: cfg.PeriodCloseRequireWatermark,
		EnablePeriodRewards: cfg.PeriodRewardsEnabled, RandomSecret: []byte(cfg.RewardRandomSecret),
		ShadowMode: cfg.RewardShadowMode,
	})
}
