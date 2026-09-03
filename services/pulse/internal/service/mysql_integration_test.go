package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
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

	userID := uint64(900000000 + time.Now().UnixNano()%1000000)
	periodKey := fmt.Sprintf("integration-action-%d", time.Now().UnixNano())
	startsAt := time.Now().Add(-time.Minute)
	endsAt := time.Now().Add(time.Hour)
	var periodID uint64
	if err := sqlDB.QueryRowContext(ctx, `
INSERT INTO pulse_period (period_key, status, starts_at, ends_at, timezone, config_version, random_version)
VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'integration-v1', 'hmac-v1')`, periodKey, startsAt, endsAt).Err(); err != nil {
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
	command := ActionCommand{UserID: userID, ActionID: "integration-action", TriggerType: "ticket", IdempotencyKey: "integration-idempotency"}

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
VALUES (?, 'new-api-log', ?, ?, 0)`, DefaultUsageCursorName, "integration-period-close", endsAt); err != nil {
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
