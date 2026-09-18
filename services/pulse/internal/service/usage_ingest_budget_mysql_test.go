package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

type rollbackUsageUnit struct {
	inner ports.UnitOfWork
	calls int
}

func (u *rollbackUsageUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	u.calls++
	return u.inner.Do(ctx, func(repos ports.Repositories) error {
		if err := fn(repos); err != nil {
			return err
		}
		if u.calls == 2 {
			return errors.New("injected transaction rollback")
		}
		return nil
	})
}

func TestMySQLUsageBudgetResumeAndRollback(t *testing.T) {
	database, db := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := fmt.Sprintf("integration-budget-%d", time.Now().UnixNano())
	at := time.Date(1998, 1, 1, 12, 0, 0, 0, time.UTC)
	insert, err := db.ExecContext(ctx, `INSERT INTO pulse_period
		(period_key, status, starts_at, ends_at, timezone, config_version, random_version)
		VALUES (?, 'active', ?, ?, 'Asia/Shanghai', 'budget-v1', 'hmac-v1')`, key, at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	id, err := insert.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`UPDATE pulse_period SET status = 'closed' WHERE id = ?`, id) })
	if _, err := db.ExecContext(ctx, `INSERT INTO pulse_economics_rule
		(period_id, rule_key, priority, model_pattern, eligible, multiplier_bps, config_version)
		VALUES (?, 'default', 0, 'gpt-*', 1, 10000, 'budget-v1')`, id); err != nil {
		t.Fatal(err)
	}
	events := make([]usage.Event, 3)
	for i := range events {
		events[i] = usage.Event{SourceSystem: "new-api-log", SourceEventID: fmt.Sprintf("%s:%d", key, i),
			CursorValue: fmt.Sprintf("%d:%d", at.Unix(), i+1), PayloadHash: fmt.Sprintf("%064d", i+1),
			UserID: 930000001, EventType: usage.EventConsume, SourceCreatedAt: at,
			QuotaDelta: 1000, ModelName: "gpt-4o", ChannelID: 2}
	}
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	newService := func(u ports.UnitOfWork) *UsageIngestService {
		svc, err := NewUsageIngestService(u, resumableUsageSource{events: events}, UsageIngestConfig{
			CursorName: key, BatchSize: 250, TicketThresholdMilli: 1000, BatchTimeBudget: 15 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		clock := at
		svc.now = func() time.Time { now := clock; clock = clock.Add(15 * time.Second); return now }
		return svc
	}
	assertProgress := func(count int) {
		t.Helper()
		var cursor string
		var rows, entries int
		var balance int64
		if err := db.QueryRowContext(ctx, `SELECT cursor_value FROM pulse_worker_cursor WHERE cursor_name = ?`, key).Scan(&cursor); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_usage_event WHERE period_id = ?`, id).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pulse_ledger_entry WHERE period_id = ?`, id).Scan(&entries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, `SELECT balance FROM pulse_account WHERE period_id = ? AND asset_type = 'contribution'`, id).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if cursor != events[count-1].CursorValue || rows != count || entries != count*2 || balance != int64(count)*1000 {
			t.Fatalf("count=%d cursor=%s rows=%d entries=%d balance=%d", count, cursor, rows, entries, balance)
		}
	}
	first, err := newService(unit).IngestBatch(ctx)
	if err != nil || !first.Yielded || first.Accepted != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	assertProgress(1)
	failed, err := newService(&rollbackUsageUnit{inner: unit}).IngestBatch(ctx)
	if err == nil || failed.Yielded || failed.Accepted != 0 || failed.TicketsMinted != 0 {
		t.Fatalf("rollback=%+v err=%v", failed, err)
	}
	assertProgress(1)
	// Reconstruct the service to prove that progress lives in MySQL, not in
	// a buffered page or mutable service state.
	for count := 2; count <= 3; count++ {
		result, err := newService(unit).IngestBatch(ctx)
		if err != nil || result.Accepted != 1 || result.Yielded != (count < 3) {
			t.Fatalf("resume=%+v err=%v", result, err)
		}
		assertProgress(count)
	}
	for i := 0; i < 100; i++ {
		result, err := newService(unit).IngestBatch(ctx)
		if err != nil || result.Fetched != 0 {
			t.Fatalf("replay=%+v err=%v", result, err)
		}
	}
	assertProgress(3)
}
