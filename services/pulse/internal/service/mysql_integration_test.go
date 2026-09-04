package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"github.com/nanashiwang/meta-pulse/migrations"
)

// These tests intentionally use a disposable MySQL database instead of a fake.
// They are skipped unless PULSE_INTEGRATION_DSN is supplied so the normal unit
// test suite remains self-contained. The DSN must point at a Pulse-only schema.
func openMySQLIntegration(t *testing.T) (*mysqlstore.DB, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set PULSE_INTEGRATION_DSN to run MySQL integration tests")
	}
	database, err := mysqlstore.Open(dsn)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrations.Up(ctx, database.SQL()); err != nil {
		t.Fatalf("apply integration migrations: %v", err)
	}
	return database, database.SQL()
}

func TestMySQLActionConcurrencyAndCommitRecovery(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Keep repeated local runs isolated: a failed test may leave its active
	// fixture behind, which would make FindActiveAt intentionally reject an
	// overlapping period. Integration schemas have no foreign keys by design.
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM pulse_period WHERE period_key LIKE 'integration-action-%'`); err != nil {
		t.Fatalf("remove stale action integration periods: %v", err)
	}
	userID := uint64(900000000 + time.Now().UnixNano()%1000000)
	periodKey := fmt.Sprintf("integration-action-%d", time.Now().UnixNano())
	startsAt := time.Now().Add(-time.Minute)
	endsAt := time.Now().Add(time.Hour)
	var periodID uint64
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, startsAt, endsAt); err != nil {
		t.Fatalf("insert period: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_period WHERE period_key = ?`, periodKey).Scan(&periodID); err != nil {
		t.Fatalf("read period id: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_definition (period_id, reward_key, reward_type, amount, weight, transferable_quota, config_version, enabled)
VALUES (?, 'integration-quota', 'quota', 5, 1, 0, 'integration-v1', 1)`, periodID); err != nil {
		t.Fatalf("insert reward definition: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_budget (period_id, budget_type, hard_cap, reserved_amount, settled_amount, released_amount, version)
VALUES (?, 'loyalty', 5, 0, 0, 0, 0)`, periodID); err != nil {
		t.Fatalf("insert reward budget: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_account (user_id, period_id, asset_type, balance, version)
VALUES (?, ?, 'ticket', 1, 1)`, userID, periodID); err != nil {
		t.Fatalf("insert ticket account: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_ledger_entry (user_id, period_id, asset_type, operation, amount, balance_after, source_type, source_ref, idempotency_key, payload_hash, reason)
VALUES (?, ?, 'ticket', 'ticket_mint', 1, 1, 'integration', ?, ?, ?, 'integration fixture')`,
		userID, periodID, periodKey, "integration-mint:"+periodKey, fmt.Sprintf("%064d", 1)); err != nil {
		t.Fatalf("insert ticket ledger fixture: %v", err)
	}

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create unit of work: %v", err)
	}
	action, err := NewActionService(unit, ActionConfig{RandomSecret: []byte("integration-action-secret"), ShadowMode: true})
	if err != nil {
		t.Fatalf("create action service: %v", err)
	}
	command := ActionCommand{UserID: userID, ActionID: "integration-action", TriggerType: ActionTriggerType, IdempotencyKey: "integration-idempotency"}

	const callers = 100
	results := make([]ActionResult, callers)
	errors := make([]error, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for i := 0; i < callers; i++ {
		go func(index int) {
			defer group.Done()
			results[index], errors[index] = action.Execute(ctx, command)
		}(i)
	}
	group.Wait()
	for i, executeErr := range errors {
		if executeErr != nil {
			t.Fatalf("concurrent action %d failed: %v", i, executeErr)
		}
		if results[i].GrantID != results[0].GrantID || results[i].RandomValue != results[0].RandomValue {
			t.Fatalf("concurrent action %d returned different result: got=%+v first=%+v", i, results[i], results[0])
		}
	}

	// A caller losing the HTTP response can safely retry with the same key.
	recovered, err := action.Execute(ctx, command)
	if err != nil {
		t.Fatalf("recover committed action: %v", err)
	}
	if recovered.GrantID != results[0].GrantID || recovered.RandomValue != results[0].RandomValue {
		t.Fatalf("recovered result changed: got=%+v first=%+v", recovered, results[0])
	}

	var grants, outboxes, spends int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_reward_grant WHERE period_id = ? AND user_id = ? AND action_id = ?`, periodID, userID, command.ActionID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_settlement_outbox WHERE reward_grant_id IN (SELECT id FROM pulse_reward_grant WHERE period_id = ? AND user_id = ? AND action_id = ?)`, periodID, userID, command.ActionID).Scan(&outboxes); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE period_id = ? AND user_id = ? AND operation = 'ticket_spend'`, periodID, userID).Scan(&spends); err != nil {
		t.Fatal(err)
	}
	if grants != 1 || outboxes != 1 || spends != 1 {
		t.Fatalf("idempotency counts grants=%d outboxes=%d spends=%d", grants, outboxes, spends)
	}

	var ticketBalance, ticketVersion int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT balance, version FROM pulse_account WHERE user_id = ? AND period_id = ? AND asset_type = 'ticket'`, userID, periodID).Scan(&ticketBalance, &ticketVersion); err != nil {
		t.Fatal(err)
	}
	var reserved, settled, released, budgetVersion int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT reserved_amount, settled_amount, released_amount, version FROM pulse_reward_budget WHERE period_id = ? AND budget_type = 'loyalty'`, periodID).Scan(&reserved, &settled, &released, &budgetVersion); err != nil {
		t.Fatal(err)
	}
	if ticketBalance != 0 || ticketVersion != 2 || reserved != 5 || settled != 0 || released != 0 || budgetVersion != 1 {
		t.Fatalf("invariant mismatch ticket=%d/%d budget=%d/%d/%d/%d", ticketBalance, ticketVersion, reserved, settled, released, budgetVersion)
	}

	ledgerService, err := NewLedgerService(unit)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ledgerService.RebuildAccount(ctx, userID, periodID, ledger.AssetTicket)
	if err != nil {
		t.Fatalf("rebuild ticket account: %v", err)
	}
	if rebuilt.Balance != 0 || rebuilt.Version != 2 {
		t.Fatalf("rebuilt ticket account = %+v", rebuilt)
	}

	var idempotencyRows int
	scope := "pulse_action:" + strconv.FormatUint(periodID, 10) + ":" + strconv.FormatUint(userID, 10)
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_idempotency WHERE scope = ? AND idempotency_key = ?`, scope, command.IdempotencyKey).Scan(&idempotencyRows); err != nil {
		t.Fatal(err)
	}
	if idempotencyRows != 1 {
		t.Fatalf("idempotency rows = %d, want 1", idempotencyRows)
	}
}

func TestMySQLPeriodCloseRollbackReentryAndRestart(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if _, err := sqlDB.ExecContext(ctx, `DELETE FROM pulse_period WHERE period_key LIKE 'integration-period-close-%'`); err != nil {
		t.Fatalf("remove stale period-close integration periods: %v", err)
	}
	userID := uint64(910000000 + time.Now().UnixNano()%1000000)
	periodKey := fmt.Sprintf("integration-period-close-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Microsecond)
	startsAt := now.Add(-time.Hour)
	endsAt := now.Add(-time.Minute)
	var periodID uint64
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, startsAt, endsAt); err != nil {
		t.Fatalf("insert period: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_period WHERE period_key = ?`, periodKey).Scan(&periodID); err != nil {
		t.Fatalf("read period id: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_definition (period_id, reward_key, reward_type, amount, weight, transferable_quota, config_version, enabled)
VALUES (?, 'integration-period-quota', 'quota', 7, 1, 0, 'integration-v1', 1)`, periodID); err != nil {
		t.Fatalf("insert reward definition: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_budget (period_id, budget_type, hard_cap, reserved_amount, settled_amount, released_amount, version)
VALUES (?, 'period_reward', 7, 0, 0, 0, 0)`, periodID); err != nil {
		t.Fatalf("insert reward budget: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_worker_cursor (cursor_name, source_system, cursor_value, watermark_at, version)
VALUES (?, 'new-api-log', ?, ?, 0)
ON DUPLICATE KEY UPDATE cursor_value = VALUES(cursor_value), watermark_at = VALUES(watermark_at)`, DefaultUsageCursorName, "integration-period-close", endsAt); err != nil {
		t.Fatalf("insert worker cursor: %v", err)
	}

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create unit of work: %v", err)
	}
	ledgerService, err := NewLedgerService(unit)
	if err != nil {
		t.Fatalf("create ledger service: %v", err)
	}
	if _, err := ledgerService.Append(ctx, ledger.Entry{
		UserID: userID, PeriodID: periodID, AssetType: ledger.AssetContribution,
		Operation: ledger.OperationContributionEarn, Amount: 1000,
		SourceType: "integration", SourceRef: periodKey + ":contribution",
		IdempotencyKey: periodKey + ":contribution", PayloadHash: fmt.Sprintf("%064d", 2),
		Reason: "integration fixture", CreatedAt: now,
	}); err != nil {
		t.Fatalf("append contribution: %v", err)
	}
	if _, err := ledgerService.Append(ctx, ledger.Entry{
		UserID: userID, PeriodID: periodID, AssetType: ledger.AssetTicket,
		Operation: ledger.OperationTicketMint, Amount: 2,
		SourceType: "integration", SourceRef: periodKey + ":ticket",
		IdempotencyKey: periodKey + ":ticket", PayloadHash: fmt.Sprintf("%064d", 3),
		Reason: "integration fixture", CreatedAt: now,
	}); err != nil {
		t.Fatalf("append ticket: %v", err)
	}

	// A stale derived snapshot must abort the whole close transaction. The
	// period transition and any economic side effects must not leak on error.
	if _, err := sqlDB.ExecContext(ctx, `
UPDATE pulse_account SET balance = balance + 1
WHERE user_id = ? AND period_id = ? AND asset_type = 'contribution'`, userID, periodID); err != nil {
		t.Fatalf("corrupt derived account fixture: %v", err)
	}
	closer, err := NewPeriodCloseService(unit, PeriodCloseConfig{
		BatchSize: 10, RequireWatermark: true, EnablePeriodRewards: true,
		RandomSecret: []byte("integration-period-close-secret"), ShadowMode: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create period close service: %v", err)
	}
	failed, err := closer.RunOnce(ctx)
	if err != nil || failed.Checked != 1 || failed.Failed != 1 || failed.Closed != 0 {
		t.Fatalf("failed close report=%+v err=%v", failed, err)
	}
	var status string
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM pulse_period WHERE id = ?`, periodID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("period status after rollback = %q, want active", status)
	}
	var sideEffects int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE period_id = ? AND operation IN ('ticket_expire')`, periodID).Scan(&sideEffects); err != nil {
		t.Fatal(err)
	}
	if sideEffects != 0 {
		t.Fatalf("close side effects after rollback = %d, want 0", sideEffects)
	}

	if _, err := ledgerService.RebuildAccount(ctx, userID, periodID, ledger.AssetContribution); err != nil {
		t.Fatalf("repair contribution account: %v", err)
	}
	closed, err := closer.RunOnce(ctx)
	if err != nil || closed.Checked != 1 || closed.Failed != 0 || closed.Closed != 1 || closed.TicketsExpired != 1 || closed.RewardsCreated != 1 {
		t.Fatalf("closed report=%+v err=%v", closed, err)
	}

	// Reopen the SQL pool to model a DB connection restart, then rerun the
	// idempotent close worker. A closed period must not create new effects.
	if err := database.Close(); err != nil {
		t.Fatalf("close integration database: %v", err)
	}
	restarted, err := mysqlstore.Open(dsn)
	if err != nil {
		t.Fatalf("reopen integration database: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if err := migrations.Up(ctx, restarted.SQL()); err != nil {
		t.Fatalf("verify migrations after restart: %v", err)
	}
	restartedUnit, err := mysqlstore.NewUnitOfWork(restarted)
	if err != nil {
		t.Fatalf("create restarted unit of work: %v", err)
	}
	restartedCloser, err := NewPeriodCloseService(restartedUnit, PeriodCloseConfig{
		BatchSize: 10, RequireWatermark: true, EnablePeriodRewards: true,
		RandomSecret: []byte("integration-period-close-secret"), ShadowMode: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create restarted close service: %v", err)
	}
	replayed, err := restartedCloser.RunOnce(ctx)
	if err != nil || replayed.Checked != 0 || replayed.Closed != 0 || replayed.RewardsCreated != 0 {
		t.Fatalf("replayed close report=%+v err=%v", replayed, err)
	}

	var expired, grants, outboxes int
	if err := restarted.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE period_id = ? AND operation = 'ticket_expire'`, periodID).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_reward_grant WHERE period_id = ? AND user_id = ? AND action_id = ?`, periodID, userID, fmt.Sprintf("period_reward:%d:%d", periodID, userID)).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_settlement_outbox WHERE reward_grant_id IN (SELECT id FROM pulse_reward_grant WHERE period_id = ? AND user_id = ?)`, periodID, userID).Scan(&outboxes); err != nil {
		t.Fatal(err)
	}
	if expired != 1 || grants != 1 || outboxes != 1 {
		t.Fatalf("replayed close duplicates expired=%d grants=%d outboxes=%d", expired, grants, outboxes)
	}
}

func TestMySQLPeriodCloseConcurrentRecheck(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	periodKey := fmt.Sprintf("integration-period-concurrent-%d", time.Now().UnixNano())
	cursorName := fmt.Sprintf("integration-period-concurrent-cursor-%d", time.Now().UnixNano())
	sourceSystem := "integration-period-close"
	startsAt := now.Add(-time.Hour)
	endsAt := now.Add(-time.Minute)
	var periodID uint64
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, startsAt, endsAt); err != nil {
		t.Fatalf("insert concurrent close period: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_period WHERE period_key = ?`, periodKey).Scan(&periodID); err != nil {
		t.Fatalf("read concurrent close period: %v", err)
	}
	t.Cleanup(func() {
		_, _ = sqlDB.Exec(`DELETE FROM pulse_worker_cursor WHERE cursor_name = ? AND source_system = ?`, cursorName, sourceSystem)
		_, _ = sqlDB.Exec(`DELETE FROM pulse_period WHERE id = ?`, periodID)
	})
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_worker_cursor (cursor_name, source_system, cursor_value, watermark_at, version)
VALUES (?, ?, '', ?, 0)`, cursorName, sourceSystem, endsAt); err != nil {
		t.Fatalf("insert concurrent close cursor: %v", err)
	}

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create concurrent close unit of work: %v", err)
	}
	newCloser := func() *PeriodCloseService {
		closer, err := NewPeriodCloseService(unit, PeriodCloseConfig{
			BatchSize: 1, CursorName: cursorName, SourceSystem: sourceSystem,
			RequireWatermark: true, Now: func() time.Time { return now },
		})
		if err != nil {
			t.Fatalf("create concurrent close service: %v", err)
		}
		return closer
	}

	reports := make([]PeriodCloseReport, 2)
	errs := make([]error, 2)
	var group sync.WaitGroup
	group.Add(2)
	for i := range reports {
		go func(index int) {
			defer group.Done()
			reports[index], errs[index] = newCloser().RunOnce(ctx)
		}(i)
	}
	group.Wait()

	closed, failed, checked := 0, 0, 0
	for i := range reports {
		if errs[i] != nil {
			t.Fatalf("concurrent period close %d returned error: %v", i, errs[i])
		}
		closed += reports[i].Closed
		failed += reports[i].Failed
		checked += reports[i].Checked
	}
	if closed != 1 || failed != 0 || checked < 1 {
		t.Fatalf("concurrent close reports=%+v closed=%d failed=%d checked=%d", reports, closed, failed, checked)
	}
	var status string
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM pulse_period WHERE id = ?`, periodID).Scan(&status); err != nil {
		t.Fatalf("read concurrent close status: %v", err)
	}
	if status != string(period.StatusClosed) {
		t.Fatalf("concurrent close status=%q", status)
	}
}

func TestMySQLUsageReplayAndConflictAreAtomic(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}
	// Use a historical instant so active periods created by other integration
	// tests cannot make FindActiveAt ambiguous. Remove leftovers from a prior
	// failed run before creating this test fixture.
	if _, err := sqlDB.ExecContext(ctx, `
DELETE FROM pulse_period WHERE period_key LIKE 'integration-usage-%'`); err != nil {
		t.Fatalf("remove stale usage integration periods: %v", err)
	}
	now := time.Date(2000, 1, 1, 12, 0, 0, 0, loc)
	userID := uint64(920000000 + time.Now().UnixNano()%1000000)
	periodKey := fmt.Sprintf("integration-usage-%d", time.Now().UnixNano())
	eventID := periodKey + ":consume"
	startsAt := now.Add(-time.Minute)
	endsAt := now.Add(time.Hour)
	var periodID uint64
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, startsAt, endsAt); err != nil {
		t.Fatalf("insert period: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_period WHERE period_key = ?`, periodKey).Scan(&periodID); err != nil {
		t.Fatalf("read period id: %v", err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"pulse_ingest_conflict", "pulse_usage_event", "pulse_ledger_entry", "pulse_account", "pulse_user_period_stat", "pulse_economics_rule"} {
			_, _ = sqlDB.Exec(`DELETE FROM `+table+` WHERE period_id = ?`, periodID)
		}
		_, _ = sqlDB.Exec(`DELETE FROM pulse_period WHERE id = ?`, periodID)
	})
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_economics_rule (period_id, rule_key, priority, model_pattern, eligible, multiplier_bps, config_version)
VALUES (?, 'integration-default', 0, 'gpt-*', 1, 10000, 'integration-v1')`, periodID); err != nil {
		t.Fatalf("insert economics rule: %v", err)
	}

	event := usage.Event{
		SourceSystem:    "new-api-log",
		SourceEventID:   eventID,
		CursorValue:     eventID,
		PayloadHash:     fmt.Sprintf("%064d", 4),
		UserID:          userID,
		EventType:       usage.EventConsume,
		SourceCreatedAt: now,
		QuotaDelta:      1500,
		ModelName:       "gpt-4o",
		ChannelID:       2,
	}
	source := staticUsageSource{events: []usage.Event{event}}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create unit of work: %v", err)
	}
	ingest, err := NewUsageIngestService(unit, source, UsageIngestConfig{BatchSize: 10, TicketThresholdMilli: 1000})
	if err != nil {
		t.Fatalf("create usage ingest service: %v", err)
	}

	first, err := ingest.IngestBatch(ctx)
	if err != nil {
		t.Fatalf("first usage ingest: %v", err)
	}
	if first.Accepted != 1 || first.TicketsMinted != 1 {
		t.Fatalf("first usage result=%+v", first)
	}
	for i := 0; i < 99; i++ {
		replayed, replayErr := ingest.IngestBatch(ctx)
		if replayErr != nil {
			t.Fatalf("usage replay %d: %v", i, replayErr)
		}
		if replayed.Replayed != 1 || replayed.Accepted != 0 || replayed.Conflicts != 0 {
			t.Fatalf("usage replay %d result=%+v", i, replayed)
		}
	}

	var usageCount, contributionEntries, ticketEntries int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_usage_event WHERE source_system = ? AND source_event_id = ?`, event.SourceSystem, event.SourceEventID).Scan(&usageCount); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE source_type = 'usage' AND source_ref = ? AND asset_type = 'contribution'`, event.SourceSystem+":"+event.SourceEventID).Scan(&contributionEntries); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE source_type = 'usage' AND source_ref = ? AND asset_type = 'ticket'`, event.SourceSystem+":"+event.SourceEventID).Scan(&ticketEntries); err != nil {
		t.Fatal(err)
	}
	if usageCount != 1 || contributionEntries != 1 || ticketEntries != 1 {
		t.Fatalf("replay created duplicates usage=%d contribution=%d ticket=%d", usageCount, contributionEntries, ticketEntries)
	}
	var contributionBalance, ticketBalance int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT balance FROM pulse_account WHERE user_id = ? AND period_id = ? AND asset_type = 'contribution'`, userID, periodID).Scan(&contributionBalance); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT balance FROM pulse_account WHERE user_id = ? AND period_id = ? AND asset_type = 'ticket'`, userID, periodID).Scan(&ticketBalance); err != nil {
		t.Fatal(err)
	}
	if contributionBalance != 1500 || ticketBalance != 1 {
		t.Fatalf("replay changed balances contribution=%d ticket=%d", contributionBalance, ticketBalance)
	}

	changed := event
	changed.PayloadHash = fmt.Sprintf("%064d", 5)
	var conflictResult IngestResult
	if err := ingest.processOne(ctx, changed, &conflictResult); err != nil {
		t.Fatalf("conflicting usage ingest: %v", err)
	}
	if conflictResult.Conflicts != 1 {
		t.Fatalf("conflict result=%+v", conflictResult)
	}
	var conflicts, entries int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ingest_conflict WHERE source_system = ? AND source_event_id = ?`, event.SourceSystem, event.SourceEventID).Scan(&conflicts); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id = ? AND period_id = ?`, userID, periodID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 || entries != 2 {
		t.Fatalf("conflict mutated accounting conflicts=%d ledger_entries=%d", conflicts, entries)
	}
}

type mysqlIntegrationBenefitClient struct {
	mu           sync.Mutex
	grantCalls   int
	queryCalls   int
	grantErr     error
	queryApplied bool
}

func (c *mysqlIntegrationBenefitClient) Grant(_ context.Context, request ports.BenefitGrantRequest) (ports.BenefitGrantResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.grantCalls++
	if c.grantErr != nil {
		return ports.BenefitGrantResponse{}, c.grantErr
	}
	return ports.BenefitGrantResponse{Applied: true, SourceRef: request.SourceRef}, nil
}

func (c *mysqlIntegrationBenefitClient) Query(_ context.Context, sourceRef string) (ports.BenefitState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryCalls++
	return ports.BenefitState{Applied: c.queryApplied, SourceRef: sourceRef}, nil
}

func (c *mysqlIntegrationBenefitClient) Rollback(_ context.Context, sourceRef, _ string) (ports.BenefitState, error) {
	return ports.BenefitState{Applied: true, SourceRef: sourceRef}, nil
}

func TestMySQLSettlementClaimAndQueryRecovery(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	periodKey := fmt.Sprintf("integration-settlement-%d", time.Now().UnixNano())
	userID := uint64(930000000 + time.Now().UnixNano()%1000000)
	var periodID uint64
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("insert period: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_period WHERE period_key = ?`, periodKey).Scan(&periodID); err != nil {
		t.Fatalf("read period id: %v", err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"pulse_settlement_outbox", "pulse_reward_grant", "pulse_reward_budget"} {
			_, _ = sqlDB.Exec(`DELETE FROM `+table+` WHERE period_id = ?`, periodID)
		}
		_, _ = sqlDB.Exec(`DELETE FROM pulse_period WHERE id = ?`, periodID)
	})
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_budget (period_id, budget_type, hard_cap, reserved_amount, settled_amount, released_amount, version)
VALUES (?, 'loyalty', 100, 25, 0, 0, 1)`, periodID); err != nil {
		t.Fatalf("insert reward budget: %v", err)
	}

	grantID := fmt.Sprintf("mysql-settlement-grant-%d", time.Now().UnixNano())
	sourceRef := grantID
	payload, err := json.Marshal(settlementPayload{GrantID: grantID, UserID: userID, Amount: 25, SourceRef: sourceRef, RewardType: "quota"})
	if err != nil {
		t.Fatalf("marshal settlement payload: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_grant (grant_id, period_id, user_id, action_id, trigger_type, reward_definition_id, reward_type, amount, random_value, config_version, status, source_ref, reason, budget_type)
VALUES (?, ?, ?, ?, 'ticket', 1, 'quota', 25, ?, 'integration-v1', 'pending', ?, 'integration fixture', 'loyalty')`, grantID, periodID, userID, grantID+":action", fmt.Sprintf("%064d", 7), sourceRef); err != nil {
		t.Fatalf("insert reward grant: %v", err)
	}
	var grantRowID uint64
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_reward_grant WHERE grant_id = ?`, grantID).Scan(&grantRowID); err != nil {
		t.Fatalf("read reward grant id: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_settlement_outbox (reward_grant_id, operation, payload_hash, payload_json, status, attempts, next_attempt_at)
VALUES (?, 'grant', ?, ?, 'pending', 0, NOW(6) - INTERVAL 1 MINUTE)`, grantRowID, canonicalJSONHash(payload), payload); err != nil {
		t.Fatalf("insert settlement outbox: %v", err)
	}

	client := &mysqlIntegrationBenefitClient{}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create unit of work: %v", err)
	}
	settlement, err := NewSettlementService(unit, client, SettlementConfig{BatchSize: 1, Lease: time.Minute, BaseBackoff: time.Second, MaxBackoff: time.Minute, MaxAttempts: 3, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create settlement service: %v", err)
	}

	const callers = 100
	reports := make([]SettlementReport, callers)
	errorsFound := make([]error, callers)
	var group sync.WaitGroup
	group.Add(callers)
	for i := 0; i < callers; i++ {
		go func(index int) {
			defer group.Done()
			reports[index], errorsFound[index] = settlement.ProcessBatch(ctx)
		}(i)
	}
	group.Wait()
	claimed, completed := 0, 0
	for i := range reports {
		if errorsFound[i] != nil {
			t.Fatalf("concurrent settlement %d failed: %v", i, errorsFound[i])
		}
		claimed += reports[i].Claimed
		completed += reports[i].Completed
	}
	client.mu.Lock()
	grantCalls, queryCalls := client.grantCalls, client.queryCalls
	client.mu.Unlock()
	if claimed != 1 || completed != 1 || grantCalls != 1 || queryCalls != 0 {
		var debugStatus, debugError string
		var debugGrantID, debugGrantSource, debugGrantType, debugBudget string
		var debugAmount, debugUser uint64
		var debugPayload, debugHash []byte
		_ = sqlDB.QueryRowContext(ctx, `SELECT status, COALESCE(last_error, '') FROM pulse_settlement_outbox WHERE reward_grant_id = ?`, grantRowID).Scan(&debugStatus, &debugError)
		_ = sqlDB.QueryRowContext(ctx, `SELECT grant_id, source_ref, reward_type, budget_type, amount, user_id FROM pulse_reward_grant WHERE id = ?`, grantRowID).Scan(&debugGrantID, &debugGrantSource, &debugGrantType, &debugBudget, &debugAmount, &debugUser)
		_ = sqlDB.QueryRowContext(ctx, `SELECT payload_json, payload_hash FROM pulse_settlement_outbox WHERE reward_grant_id = ?`, grantRowID).Scan(&debugPayload, &debugHash)
		t.Fatalf("concurrent settlement claimed=%d completed=%d grant_calls=%d query_calls=%d outbox=%q error=%q grant=%q/%q/%q/%q amount=%d user=%d payload=%s hash=%s", claimed, completed, grantCalls, queryCalls, debugStatus, debugError, debugGrantID, debugGrantSource, debugGrantType, debugBudget, debugAmount, debugUser, debugPayload, debugHash)
	}

	var grantStatus, outboxStatus string
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM pulse_reward_grant WHERE id = ?`, grantRowID).Scan(&grantStatus); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM pulse_settlement_outbox WHERE reward_grant_id = ?`, grantRowID).Scan(&outboxStatus); err != nil {
		t.Fatal(err)
	}
	if grantStatus != GrantStatusSettled || outboxStatus != OutboxStatusCompleted {
		t.Fatalf("settlement states grant=%q outbox=%q", grantStatus, outboxStatus)
	}

	var spentTickets, statVersion int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT spent_tickets, version FROM pulse_user_period_stat WHERE user_id = ? AND period_id = ?`, userID, periodID).Scan(&spentTickets, &statVersion); err != nil {
		t.Fatalf("read action ticket stat: %v", err)
	}
	if spentTickets != 1 || statVersion != 1 {
		t.Fatalf("action ticket stat spent=%d version=%d", spentTickets, statVersion)
	}

	// A successful Benefit call followed by a Pulse-side timeout is recovered
	// by querying the same source_ref; no new grant or source_ref is created.
	secondUserID := userID + 1
	secondGrantID := fmt.Sprintf("mysql-settlement-query-%d", time.Now().UnixNano())
	secondPayload, err := json.Marshal(settlementPayload{GrantID: secondGrantID, UserID: secondUserID, Amount: 9, SourceRef: secondGrantID, RewardType: "quota"})
	if err != nil {
		t.Fatalf("marshal query recovery payload: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `UPDATE pulse_reward_budget SET reserved_amount = 9, version = 2 WHERE period_id = ? AND budget_type = 'loyalty'`, periodID); err != nil {
		t.Fatalf("reserve query recovery budget: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_grant (grant_id, period_id, user_id, action_id, trigger_type, reward_definition_id, reward_type, amount, random_value, config_version, status, source_ref, reason, budget_type)
VALUES (?, ?, ?, ?, 'ticket', 1, 'quota', 9, ?, 'integration-v1', 'pending', ?, 'integration query fixture', 'loyalty')`, secondGrantID, periodID, secondUserID, secondGrantID+":action", fmt.Sprintf("%064d", 8), secondGrantID); err != nil {
		t.Fatalf("insert query recovery grant: %v", err)
	}
	var secondGrantRowID uint64
	if err := sqlDB.QueryRowContext(ctx, `SELECT id FROM pulse_reward_grant WHERE grant_id = ?`, secondGrantID).Scan(&secondGrantRowID); err != nil {
		t.Fatalf("read query recovery grant id: %v", err)
	}
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_settlement_outbox (reward_grant_id, operation, payload_hash, payload_json, status, attempts, next_attempt_at)
VALUES (?, 'grant', ?, ?, 'pending', 0, NOW(6) - INTERVAL 1 MINUTE)`, secondGrantRowID, canonicalJSONHash(secondPayload), secondPayload); err != nil {
		t.Fatalf("insert query recovery outbox: %v", err)
	}
	client.mu.Lock()
	client.grantErr = errors.New("simulated benefit timeout")
	client.queryApplied = true
	client.mu.Unlock()
	recovered, err := settlement.ProcessBatch(ctx)
	if err != nil || recovered.Claimed != 1 || recovered.Completed != 1 || recovered.Retried != 0 {
		t.Fatalf("query recovery report=%+v err=%v", recovered, err)
	}
	client.mu.Lock()
	grantCalls, queryCalls = client.grantCalls, client.queryCalls
	client.mu.Unlock()
	if grantCalls != 2 || queryCalls != 1 {
		t.Fatalf("query recovery calls grant=%d query=%d", grantCalls, queryCalls)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT status FROM pulse_reward_grant WHERE id = ?`, secondGrantRowID).Scan(&grantStatus); err != nil {
		t.Fatal(err)
	}
	if grantStatus != GrantStatusSettled {
		t.Fatalf("query recovered grant status=%q", grantStatus)
	}
	var reserved, settled, budgetVersion int64
	if err := sqlDB.QueryRowContext(ctx, `SELECT reserved_amount, settled_amount, version FROM pulse_reward_budget WHERE period_id = ? AND budget_type = 'loyalty'`, periodID).Scan(&reserved, &settled, &budgetVersion); err != nil {
		t.Fatal(err)
	}
	if reserved != 0 || settled != 34 || budgetVersion != 3 {
		t.Fatalf("settlement budget reserved=%d settled=%d version=%d", reserved, settled, budgetVersion)
	}
}

func TestMySQLSettlementLeaseRecoveryAndFence(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	rewardGrantID := uint64(950000000 + time.Now().UnixNano()%1000000)
	result, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_settlement_outbox (reward_grant_id, operation, payload_hash, payload_json, status, attempts, next_attempt_at, leased_until)
VALUES (?, 'grant', ?, JSON_OBJECT(), 'processing', 1, ?, ?)`,
		rewardGrantID, fmt.Sprintf("%064d", 9), now.Add(-time.Minute), now.Add(-time.Second))
	if err != nil {
		t.Fatalf("insert expired settlement lease: %v", err)
	}
	insertedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read expired settlement lease id: %v", err)
	}
	outboxID := uint64(insertedID)
	t.Cleanup(func() { _, _ = sqlDB.Exec(`DELETE FROM pulse_settlement_outbox WHERE id = ?`, outboxID) })

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create unit of work: %v", err)
	}
	var reclaimed ports.SettlementOutbox
	if err := unit.Do(ctx, func(repos ports.Repositories) error {
		claimed, err := repos.Settlement.ClaimDue(ctx, now, 1, now.Add(time.Minute))
		if err == nil {
			if len(claimed) != 1 {
				return fmt.Errorf("expected one reclaimed outbox, got %d", len(claimed))
			}
			reclaimed = claimed[0]
		}
		return err
	}); err != nil {
		t.Fatalf("reclaim expired settlement lease: %v", err)
	}
	if reclaimed.ID != outboxID || reclaimed.Status != OutboxStatusProcessing || reclaimed.Attempts != 2 || reclaimed.LeasedUntil == nil {
		t.Fatalf("unexpected reclaimed outbox: %+v", reclaimed)
	}

	// The original worker still carries attempts=1. Its late write must not
	// overwrite the lease owner that reclaimed the row with attempts=2.
	stale := reclaimed
	stale.Attempts = 1
	stale.Status = OutboxStatusCompleted
	stale.LeasedUntil = nil
	stale.CompletedAt = &now
	if err := unit.Do(ctx, func(repos ports.Repositories) error {
		return repos.Settlement.SaveOutbox(ctx, stale)
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("stale settlement write error=%v, want ErrNotFound", err)
	}

	reclaimed.Status = OutboxStatusCompleted
	reclaimed.LeasedUntil = nil
	reclaimed.CompletedAt = &now
	if err := unit.Do(ctx, func(repos ports.Repositories) error {
		return repos.Settlement.SaveOutbox(ctx, reclaimed)
	}); err != nil {
		t.Fatalf("save reclaimed settlement: %v", err)
	}

	var status string
	var attempts uint32
	if err := sqlDB.QueryRowContext(ctx, `SELECT status, attempts FROM pulse_settlement_outbox WHERE id = ?`, outboxID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read reclaimed settlement: %v", err)
	}
	if status != OutboxStatusCompleted || attempts != 2 {
		t.Fatalf("persisted settlement status=%q attempts=%d", status, attempts)
	}
}

func TestMySQLContentAwardConcurrentCaps(t *testing.T) {
	database, sqlDB := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The integration schema is disposable but may be reused locally. Remove
	// this test's old fixtures so prior runs cannot consume today's global cap
	// or leave an overlapping active period behind.
	for _, query := range []string{
		`DELETE FROM pulse_content_award WHERE action_id LIKE 'content_award:question:cap-%'`,
		`DELETE FROM pulse_content_candidate WHERE source_content_id LIKE 'cap-%'`,
		`DELETE FROM pulse_period WHERE period_key LIKE 'integration-content-cap-%'`,
	} {
		if _, err := sqlDB.ExecContext(ctx, query); err != nil {
			t.Fatalf("clean old content cap fixtures: %v", err)
		}
	}

	t.Run("daily cap across users", func(t *testing.T) {
		now := time.Now().In(time.FixedZone("CST", 8*60*60)).Truncate(time.Second)
		runMySQLContentAwardCapCase(t, database, sqlDB, now, now, false, 100, 10)
	})
	t.Run("user period cap across days", func(t *testing.T) {
		now := time.Now().In(time.FixedZone("CST", 8*60*60)).Truncate(time.Second)
		runMySQLContentAwardCapCase(t, database, sqlDB, now, now.AddDate(0, 0, 1), true, 10, 100)
	})
}

func runMySQLContentAwardCapCase(t *testing.T, database *mysqlstore.DB, sqlDB *sql.DB, firstNow, secondNow time.Time, sameUser bool, maxUser, maxDaily int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	periodKey := fmt.Sprintf("integration-content-cap-%d", suffix)
	result, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'draft', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, firstNow.Add(-time.Hour), secondNow.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("insert content cap period: %v", err)
	}
	periodRaw, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read content cap period id: %v", err)
	}
	periodID := uint64(periodRaw)
	if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_reward_budget (period_id, budget_type, hard_cap, reserved_amount, settled_amount, released_amount, version)
VALUES (?, 'content_reward', 100, 0, 0, 0, 0)`, periodID); err != nil {
		t.Fatalf("insert content cap budget: %v", err)
	}

	firstUser := uint64(1_100_000_000 + suffix%100_000_000)
	secondUser := firstUser
	if !sameUser {
		secondUser++
	}
	users := []uint64{firstUser}
	if secondUser != firstUser {
		users = append(users, secondUser)
	}
	for _, userID := range users {
		if _, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_account (user_id, period_id, asset_type, balance, version)
VALUES (?, ?, 'contribution', 1000, 1)`, userID, periodID); err != nil {
			t.Fatalf("insert content cap account: %v", err)
		}
	}

	candidateIDs := make([]uint64, 2)
	for i, userID := range []uint64{firstUser, secondUser} {
		candidateResult, err := sqlDB.ExecContext(ctx, `
INSERT INTO pulse_content_candidate
(source_system, source_content_id, content_type, author_user_id, period_id, title, source_created_at, payload_hash, cursor_value, status)
VALUES ('answer-forum', ?, 'question', ?, ?, 'cap test', ?, ?, ?, 'pending')`,
			fmt.Sprintf("cap-%d-%d", suffix, i), userID, periodID, firstNow, fmt.Sprintf("%064d", i+1), fmt.Sprintf("%d", i+1))
		if err != nil {
			t.Fatalf("insert content cap candidate: %v", err)
		}
		candidateID, err := candidateResult.LastInsertId()
		if err != nil {
			t.Fatalf("read content cap candidate id: %v", err)
		}
		candidateIDs[i] = uint64(candidateID)
	}

	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatalf("create content cap unit: %v", err)
	}
	newService := func(now time.Time) *ContentAwardService {
		service, err := NewContentAwardService(unit, ContentAwardConfig{
			MinPaidContributionMilli: 1,
			MaxUserPeriodAmount:      maxUser,
			MaxDailyAmount:           maxDaily,
			BudgetType:               "content_reward",
			ConfigVersion:            "integration-v1",
			ShadowMode:               true,
			Now:                      func() time.Time { return now },
		})
		if err != nil {
			t.Fatalf("create content cap service: %v", err)
		}
		return service
	}
	services := []*ContentAwardService{newService(firstNow), newService(secondNow)}
	type outcome struct {
		result ContentAwardResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	for i := range services {
		i := i
		go func() {
			<-start
			result, err := services[i].ReviewAndAward(ctx, ContentAwardCommand{
				CandidateID: candidateIDs[i], AwardVersion: 1, PeriodID: periodID,
				RewardType: "quota", Amount: 10, Reason: "并发限额验收",
				ActorType: "admin", ActorID: fmt.Sprintf("op-%d", i), RequestID: fmt.Sprintf("cap-%d-%d", suffix, i),
			})
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)

	eligible, limited := 0, 0
	for i := 0; i < 2; i++ {
		outcome := <-outcomes
		if outcome.err != nil {
			t.Fatalf("concurrent content award: %v", outcome.err)
		}
		switch outcome.result.Eligibility {
		case "eligible":
			eligible++
		case ports.ContentAwardLimited:
			limited++
		default:
			t.Fatalf("unexpected content eligibility %q", outcome.result.Eligibility)
		}
	}
	if eligible != 1 || limited != 1 {
		t.Fatalf("eligible=%d limited=%d, want 1/1", eligible, limited)
	}

	var activeAmount, grantCount, reservedAmount int64
	if err := sqlDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(amount), 0) FROM pulse_content_award
WHERE period_id = ? AND status IN ('pending', 'settled')`, periodID).Scan(&activeAmount); err != nil {
		t.Fatalf("read active content amount: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_reward_grant WHERE period_id = ? AND budget_type = 'content_reward'`, periodID).Scan(&grantCount); err != nil {
		t.Fatalf("read content grant count: %v", err)
	}
	if err := sqlDB.QueryRowContext(ctx, `SELECT reserved_amount FROM pulse_reward_budget WHERE period_id = ? AND budget_type = 'content_reward'`, periodID).Scan(&reservedAmount); err != nil {
		t.Fatalf("read content reserved amount: %v", err)
	}
	if activeAmount != 10 || grantCount != 1 || reservedAmount != 10 {
		t.Fatalf("active=%d grants=%d reserved=%d, want 10/1/10", activeAmount, grantCount, reservedAmount)
	}
}
