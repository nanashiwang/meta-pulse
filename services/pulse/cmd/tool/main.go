package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/adapter/newapi"
	"github.com/nanashiwang/meta-pulse/internal/config"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
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
	case "config-check":
		if err := runConfigCheck(os.Args[2:]); err != nil {
			logger.Error("configuration check failed", "error", err)
			os.Exit(1)
		}
	case "migrate-up", "migrate-status":
		if err := runMigration(os.Args[1] == "migrate-up"); err != nil {
			logger.Error("migration command failed", "error", err)
			os.Exit(1)
		}
	case "access-check":
		if err := runAccessCheck(); err != nil {
			logger.Error("access check failed", "error", err)
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
	case "backtest":
		if err := runBacktest(os.Args[2:]); err != nil {
			logger.Error("backtest failed", "error", err)
			os.Exit(1)
		}
	case "reward-retry":
		if err := runRewardRetry(os.Args[2:]); err != nil {
			logger.Error("reward retry failed", "error", err)
			os.Exit(1)
		}
	case "reconcile":
		if err := runReconcile(); err != nil {
			logger.Error("reconciliation failed", "error", err)
			os.Exit(1)
		}
	case "period-close":
		if err := runPeriodClose(); err != nil {
			logger.Error("period close failed", "error", err)
			os.Exit(1)
		}
	case "period-create":
		if err := runPeriodCreate(os.Args[2:]); err != nil {
			logger.Error("period create failed", "error", err)
			os.Exit(1)
		}
	case "cursor-seek":
		if err := runCursorSeek(os.Args[2:]); err != nil {
			logger.Error("cursor seek failed", "error", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func runReconcile() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateWorker(); err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	client, err := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
	if err != nil {
		return err
	}
	settlement, err := service.NewSettlementService(unit, client, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report, err := settlement.Reconcile(ctx)
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return err
}

func parseRewardRetryArgs(args []string) (string, error) {
	flags := flag.NewFlagSet("reward-retry", flag.ContinueOnError)
	grantID := flags.String("grant-id", "", "public reward grant id/source_ref to retry")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	value := strings.TrimSpace(*grantID)
	if value == "" {
		return "", errors.New("--grant-id is required")
	}
	return value, nil
}

func runRewardRetry(args []string) error {
	grantID, err := parseRewardRetryArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateWorker(); err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	client, err := newapi.NewBenefitClient(cfg.NewAPIInternalURL, []byte(cfg.ServiceHMACSecret), nil)
	if err != nil {
		return err
	}
	settlement, err := service.NewSettlementService(unit, client, service.SettlementConfig{BatchSize: cfg.SettlementBatchSize})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report, err := settlement.RetryDead(ctx, grantID)
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return err
}

func runBacktest(args []string) error {
	flags := flag.NewFlagSet("backtest", flag.ContinueOnError)
	fromValue := flags.String("from", "", "start time in RFC3339 (inclusive)")
	toValue := flags.String("to", "", "end time in RFC3339 (exclusive)")
	manualMultiplier := flags.Int64("manual-multiplier-bps", 10000, "manual comparison multiplier in basis points")
	if err := flags.Parse(args); err != nil {
		return err
	}
	parseTime := func(name, value string) (time.Time, error) {
		if value == "" {
			return time.Time{}, nil
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --%s: %w", name, err)
		}
		return parsed, nil
	}
	from, err := parseTime("from", *fromValue)
	if err != nil {
		return err
	}
	to, err := parseTime("to", *toValue)
	if err != nil {
		return err
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
	backtest, err := service.NewBacktestService(unit, source, service.BacktestConfig{
		BatchSize:            cfg.IngestBatchSize,
		TicketThresholdMilli: cfg.TicketThresholdMilli,
		ManualMultiplierBps:  money.Bps(*manualMultiplier),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report, runErr := backtest.Run(ctx, from, to)
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return runErr
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

func runPeriodClose() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := mysqlstore.Open(cfg.PulseDBDSN)
	if err != nil {
		return err
	}
	defer database.Close()

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	periodCloser, err := service.NewPeriodCloseService(unit, service.PeriodCloseConfig{
		BatchSize:           cfg.PeriodCloseBatchSize,
		CursorName:          service.DefaultUsageCursorName,
		SourceSystem:        "new-api-log",
		RequireWatermark:    cfg.PeriodCloseRequireWatermark,
		EnablePeriodRewards: cfg.PeriodRewardsEnabled,
		RandomSecret:        []byte(cfg.RewardRandomSecret),
		ShadowMode:          cfg.RewardShadowMode,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	report, runErr := periodCloser.RunOnce(ctx)
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return runErr
}

func runAccessCheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Permission checks must not require Benefit connectivity or its HMAC
	// secret; those are unrelated to proving LOG_DB is read-only.
	if err := cfg.ValidateLogReader(); err != nil {
		return err
	}
	reader, err := newapi.OpenLogReader(cfg.NewAPILogDSN)
	if err != nil {
		return err
	}
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := reader.CheckReadOnly(ctx)
	if err != nil {
		return err
	}
	encoded, encodeErr := json.MarshalIndent(report, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	if !report.Readable || !report.ReadOnly {
		return fmt.Errorf("new-api LOG_DB account is not a proven read-only account")
	}
	return nil
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
	fmt.Println("commands: config-check --role <api|worker|tool> | migrate-up | migrate-status | backfill | backtest | access-check | reconcile | ledger-check | period-close | period-create | cursor-seek | reward-retry --grant-id <pg_...>")
}

// runConfigCheck never opens a database or emits credential values.
func runConfigCheck(args []string) error {
	flags := flag.NewFlagSet("config-check", flag.ContinueOnError)
	role := flags.String("role", "", "required process role: api, worker or tool")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected config-check arguments")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return cfg.ValidateRole(*role)
}

// runPeriodCreate writes a period and its frozen rules. --activate is opt-in
// so an operator can review the draft in the database before usage events can
// be assigned to it.
func runPeriodCreate(args []string) error {
	flags := flag.NewFlagSet("period-create", flag.ContinueOnError)
	key := flags.String("key", "", "unique period key, e.g. 2026-P001")
	startsAt := flags.String("starts-at", "", "period start in RFC3339, e.g. 2026-09-16T00:00:00+08:00")
	timezone := flags.String("timezone", "Asia/Shanghai", "period timezone")
	configVersion := flags.String("config-version", "", "immutable economics config version")
	randomVersion := flags.String("random-version", "", "immutable random version (defaults to config version)")
	multiplierBps := flags.Int("multiplier-bps", 0, "catch-all contribution multiplier in basis points (10000 = 1.0x)")
	ruleKey := flags.String("rule-key", "default", "catch-all rule key")
	activate := flags.Bool("activate", false, "activate the period immediately")
	reason := flags.String("reason", "", "required audit reason")
	actorID := flags.String("actor-id", "", "required operator identity for the audit log")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *startsAt == "" {
		return errors.New("--starts-at is required")
	}
	start, err := time.Parse(time.RFC3339, *startsAt)
	if err != nil {
		return fmt.Errorf("invalid --starts-at: %w", err)
	}
	if *multiplierBps <= 0 {
		return errors.New("--multiplier-bps must be positive; a zero multiplier would record every event with zero contribution")
	}
	if *multiplierBps > int(money.MaxBps) {
		return fmt.Errorf("--multiplier-bps exceeds the maximum of %d", money.MaxBps)
	}
	if *randomVersion == "" {
		*randomVersion = *configVersion
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
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	creator, err := service.NewPeriodCreateService(unit, time.Now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := creator.Create(ctx, service.PeriodCreateCommand{
		ActorType: "operator", ActorID: *actorID, Key: *key, StartsAt: start,
		Timezone: *timezone, ConfigVersion: *configVersion, RandomVersion: *randomVersion,
		Rules: []service.PeriodRuleSpec{{
			Key: *ruleKey, Priority: 0, Eligible: true, MultiplierBps: int32(*multiplierBps),
		}},
		Activate: *activate, Reason: *reason,
	})
	if err != nil {
		return err
	}
	encoded, encodeErr := json.MarshalIndent(result, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return nil
}

// runCursorSeek forfeits the backlog before --skip-before. Those log rows are
// never ingested, so they produce no contribution, tickets or rewards.
func runCursorSeek(args []string) error {
	flags := flag.NewFlagSet("cursor-seek", flag.ContinueOnError)
	skipBefore := flags.String("skip-before", "", "resume ingestion at this RFC3339 time, forfeiting everything earlier")
	cursorName := flags.String("cursor-name", service.DefaultUsageCursorName, "worker cursor name")
	sourceSystem := flags.String("source-system", "new-api-log", "cursor source system")
	reason := flags.String("reason", "", "required audit reason")
	actorID := flags.String("actor-id", "", "required operator identity for the audit log")
	confirm := flags.Bool("confirm", false, "required: acknowledge that skipped log rows are permanently forfeited")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*confirm {
		return errors.New("--confirm is required: skipped rows never produce contribution, tickets or rewards")
	}
	if *skipBefore == "" {
		return errors.New("--skip-before is required")
	}
	target, err := time.Parse(time.RFC3339, *skipBefore)
	if err != nil {
		return fmt.Errorf("invalid --skip-before: %w", err)
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
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		return err
	}
	seeker, err := service.NewCursorSeekService(unit, time.Now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := seeker.Seek(ctx, service.CursorSeekCommand{
		ActorType: "operator", ActorID: *actorID, CursorName: *cursorName,
		SourceSystem: *sourceSystem, SkipBefore: target, Reason: *reason,
	})
	if err != nil {
		return err
	}
	encoded, encodeErr := json.MarshalIndent(result, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	fmt.Println(string(encoded))
	return nil
}
