package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

type rollbackFundingUnit struct {
	unit    ports.UnitOfWork
	failure error
}

func (u rollbackFundingUnit) Do(ctx context.Context, callback func(ports.Repositories) error) error {
	return u.unit.Do(ctx, func(repos ports.Repositories) error {
		if err := callback(repos); err != nil {
			return err
		}
		return u.failure
	})
}

func TestMySQLFundingProofReplayAndTransactionRollback(t *testing.T) {
	database, db := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	creator, _ := NewPeriodCreateService(unit, time.Now)
	command := fundedPeriodCommand()
	command.Key = fmt.Sprintf("integration-proof-%d", time.Now().UnixNano())
	command.StartsAt = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	var previous sql.NullTime
	if err := db.QueryRowContext(ctx, "SELECT MAX(ends_at) FROM pulse_period WHERE period_key LIKE 'integration-proof-%'").Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if previous.Valid {
		command.StartsAt = previous.Time.Add(time.Hour)
	}
	activity, err := creator.Create(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("UPDATE pulse_period SET status='closed' WHERE id=?", activity.PeriodID) })
	userID := uint64(2_000_000_000 + time.Now().UnixNano()%100_000_000)
	event := usage.Event{SourceSystem: "new-api-log", SourceEventID: command.Key + ":first", CursorValue: "1:1", PayloadHash: fmt.Sprintf("%064x", userID), FundingProof: command.Key + ":wallet", UserID: userID, EventType: usage.EventConsume, SourceCreatedAt: command.StartsAt.Add(time.Hour), QuotaDelta: 1500, ModelName: "gpt-test", ChannelID: 1}
	cfg := UsageIngestConfig{BatchSize: 100, CursorName: command.Key, TicketThresholdMilli: 1000}
	injected := errors.New("test abort after all writes before commit")
	failing, _ := NewUsageIngestService(rollbackFundingUnit{unit: unit, failure: injected}, staticUsageSource{}, cfg)
	var attempt IngestResult
	if err := failing.processOne(ctx, event, &attempt); !errors.Is(err, injected) {
		t.Fatalf("failure=%v", err)
	}
	for _, check := range []struct {
		query string
		args  []any
	}{
		{"SELECT COUNT(*) FROM pulse_idempotency WHERE scope=? AND idempotency_key=?", []any{"paid_funding:new-api-log", event.FundingProof}},
		{"SELECT COUNT(*) FROM pulse_usage_event WHERE source_system=? AND source_event_id=?", []any{event.SourceSystem, event.SourceEventID}},
		{"SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND period_id=?", []any{userID, activity.PeriodID}},
		{"SELECT COUNT(*) FROM pulse_account WHERE user_id=? AND period_id=?", []any{userID, activity.PeriodID}},
		{"SELECT COUNT(*) FROM pulse_user_period_stat WHERE user_id=? AND period_id=?", []any{userID, activity.PeriodID}},
		{"SELECT COUNT(*) FROM pulse_worker_cursor WHERE cursor_name=?", []any{command.Key}},
	} {
		var n int
		if err := db.QueryRowContext(ctx, check.query, check.args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("partial accounting survived rollback: %s = %d", check.query, n)
		}
	}
	ingest, _ := NewUsageIngestService(unit, staticUsageSource{}, cfg)
	var result IngestResult
	if err := ingest.processOne(ctx, event, &result); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := ingest.processOne(ctx, event, &result); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	duplicate := event
	duplicate.SourceEventID = command.Key + ":duplicate"
	duplicate.CursorValue = "1:2"
	// Even an identical payload hash cannot reuse a consumed funding proof
	// under a new source event identity.
	if err := ingest.processOne(ctx, duplicate, &result); err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 1 || result.Replayed != 100 || result.ManualReview != 1 || result.TicketsMinted != 1 {
		t.Fatalf("result=%+v", result)
	}
	var entries int
	var contribution, tickets int64
	var owner, status string
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND period_id=?", userID, activity.PeriodID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT balance FROM pulse_account WHERE user_id=? AND period_id=? AND asset_type='contribution'", userID, activity.PeriodID).Scan(&contribution); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT balance FROM pulse_account WHERE user_id=? AND period_id=? AND asset_type='ticket'", userID, activity.PeriodID).Scan(&tickets); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT resource_id FROM pulse_idempotency WHERE scope=? AND idempotency_key=?", "paid_funding:new-api-log", event.FundingProof).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT status FROM pulse_usage_event WHERE source_system=? AND source_event_id=?", event.SourceSystem, duplicate.SourceEventID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if entries != 2 || contribution != 1500 || tickets != 1 || owner != event.SourceEventID || status != "manual_review" {
		t.Fatalf("entries=%d contribution=%d tickets=%d owner=%q status=%q", entries, contribution, tickets, owner, status)
	}
}
