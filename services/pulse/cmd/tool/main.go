package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/service"
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
	case "ledger-check":
		if err := runLedgerCheck(); err != nil {
			logger.Error("ledger check failed", "error", err)
			os.Exit(1)
		}
	case "backfill":
		if err := runBackfill(os.Args[2:]); err != nil {
			logger.Error("backfill failed", "error", err)
			os.Exit(1)
		}
	case "backtest", "reconcile", "period-close", "reward-retry":
		fmt.Printf("meta-pulse tool %s: command scaffold; implementation pending\n", os.Args[1])
	default:
		usage()
	}
}

func runBackfill(args []string) error {
	flags := flag.NewFlagSet("backfill", flag.ContinueOnError)
	fromValue := flags.String("from", "", "start time in RFC3339")
	toValue := flags.String("to", "", "end time in RFC3339")
	dryRun := flags.Bool("dry-run", false, "report only; do not write Pulse data")
	if err := flags.Parse(args); err != nil {
		return err
	}
	parseTime := func(value string) (time.Time, error) {
		if value == "" {
			return time.Time{}, nil
		}
		return time.Parse(time.RFC3339, value)
	}
	from, err := parseTime(*fromValue)
	if err != nil {
		return fmt.Errorf("invalid --from: %w", err)
	}
	to, err := parseTime(*toValue)
	if err != nil {
		return fmt.Errorf("invalid --to: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	reader, err := newapi.OpenLogReader(cfg.NewAPILogDSN)
	if err != nil {
		return err
	}
	defer reader.Close()
	source, err := newapi.NewLogSource(reader, "new-api-log")
	if err != nil {
		return err
	}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	ingest, err := service.NewUsageIngestService(unit, source, service.UsageIngestConfig{CursorName: "new-api-usage-backfill", SourceSystem: "new-api-log", BatchSize: cfg.IngestBatchSize, TicketThresholdMilli: cfg.TicketThresholdMilli})
	if err != nil {
		return err
	}
	backfill, err := service.NewBackfillService(ingest, source)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report, err := backfill.Run(ctx, service.BackfillOptions{From: from, To: to, DryRun: *dryRun})
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return err
}

func runLedgerCheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	report, checkErr := database.CheckLedger(ctx)
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return checkErr
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
