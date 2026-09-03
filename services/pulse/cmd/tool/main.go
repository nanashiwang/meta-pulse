package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/config"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"github.com/nanashiwang/meta-pulse/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "migrate-up", "migrate-status":
		if err := runMigration(os.Args[1] == "migrate-up"); err != nil {
			logger.Error("migration command failed", "error", err)
			os.Exit(1)
		}
	case "backfill", "backtest", "reconcile", "ledger-check", "period-close", "reward-retry":
		fmt.Printf("meta-pulse tool %s: command scaffold; implementation pending\n", os.Args[1])
	default:
		usage()
	}
}

func runMigration(up bool) error {
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
	if up {
		return migrations.Up(ctx, database.SQL())
	}
	return migrations.Status(ctx, database.SQL())
}

func usage() {
	fmt.Println("Meta Pulse operator tool")
	fmt.Println("commands: migrate-up | migrate-status | backfill | backtest | reconcile | ledger-check | period-close | reward-retry")
}
