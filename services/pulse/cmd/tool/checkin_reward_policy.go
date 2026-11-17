package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
	"github.com/nanashiwang/meta-pulse/internal/service"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func runCheckinRewardPolicy() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	manager, err := runtimeconfig.New(ctx, mysqlstore.NewRuntimeConfigStore(database), runtimeconfig.RoleAPI, cfg.RuntimeKeyDir, cfg)
	if err != nil {
		return err
	}
	cfg, err = manager.Current(ctx)
	if err != nil {
		return err
	}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	creator, err := service.NewPeriodCreateService(unit, time.Now)
	if err != nil {
		return err
	}
	result, err := creator.ApplyCheckinRewardPolicy(ctx, cfg.QuotaPerUnit)
	if err != nil {
		return err
	}
	bonus, err := creator.ApplyDrawBonusPolicy(ctx, cfg.QuotaPerUnit)
	if err != nil {
		return err
	}
	if bonus.PeriodID != 0 {
		result = bonus
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
