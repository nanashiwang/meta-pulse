package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	redisstore "github.com/nanashiwang/meta-pulse/internal/store/redis"
)

// Preflight does not initialize runtime keys, migrate schemas or contact Benefit.
func runInfrastructureCheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return errors.New("Pulse database configuration unavailable")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if db.Ping(ctx) != nil {
		return errors.New("Pulse database connection failed")
	}
	cache, err := redisstore.OpenConfigured(cfg.RedisURL, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		return err
	}
	defer cache.Close()
	if cache.Ping(ctx) != nil {
		return errors.New("Redis connection failed")
	}
	fmt.Println("Pulse MySQL/Redis preflight passed")
	return nil
}
