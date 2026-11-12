package mysql

import (
	"context"
	"fmt"
	"github.com/nanashiwang/meta-pulse/migrations"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMySQLTicketStatistics(t *testing.T) {
	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("requires isolated integration database")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = migrations.Up(context.Background(), db.SQL()); err != nil {
		t.Fatal(err)
	}
	// Simulate MySQL completing DDL after the caller timed out, before Goose
	// recorded version 20. Retrying must preserve the existing index and data.
	if _, err = db.SQL().Exec("DELETE FROM goose_db_version WHERE version_id>=20"); err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(context.Background(), db.SQL()); err != nil {
		t.Fatalf("resume completed statistics index: %v", err)
	}
	tx := db.GORM().Begin()
	defer tx.Rollback()
	// Read projections must include historical mint/bonus/adjustment, but never
	// count spend/reversal/expiry as newly received tickets.
	if err = tx.Exec("DELETE FROM pulse_ledger_entry WHERE user_id=987654321").Error; err != nil {
		t.Fatal(err)
	}
	for i, e := range []struct {
		op string
		n  int64
	}{{"ticket_mint", 10}, {"ticket_bonus", 2}, {"ticket_spend", -3}, {"ticket_expire", -2}, {"ticket_reverse", -1}, {"ticket_adjustment", 4}, {"ticket_adjustment", -1}} {
		err = tx.Exec(`INSERT INTO pulse_ledger_entry(user_id,period_id,asset_type,operation,amount,balance_after,source_type,source_ref,idempotency_key,payload_hash,created_at) VALUES(987654321,1,'ticket',?,?,0,'statistics-test','fixture',?,REPEAT('a',64),NOW())`, e.op, e.n, i).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	// This user's old 258911 tickets were reset; only the later single ticket was spent.
	for i, e := range []struct {
		op string
		n  int64
	}{{"ticket_mint", 258911}, {"ticket_adjustment", -258911}, {"ticket_bonus", 1}, {"ticket_spend", -1}} {
		if err = tx.Exec(`INSERT INTO pulse_ledger_entry(user_id,period_id,asset_type,operation,amount,balance_after,source_type,source_ref,idempotency_key,payload_hash,created_at) VALUES(987654323,1,'ticket',?,?,0,'statistics-reset','fixture',?,REPEAT('b',64),NOW())`, e.op, e.n, fmt.Sprintf("reset-%d", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for i, e := range []struct {
		kind, status, trigger string
		amount                int64
		reversed              *time.Time
	}{
		{"newapi_quota", "settled", "pulse", 625000, nil}, {"community_exp", "settled", "pulse", 20, nil},
		{"newapi_quota", "pending", "pulse", 999999, nil}, {"community_exp", "reversed", "pulse", 999999, nil},
		{"newapi_quota", "settled", "pulse", 999999, &now}, {"newapi_quota", "settlement_dead", "pulse", 999999, nil},
		{"newapi_quota", "settled", "content", 999999, nil}, {"community_exp", "reversal_pending", "pulse", 999999, nil},
	} {
		key := fmt.Sprintf("statistics-reward-%d", i)
		row := rewardGrantModel{GrantID: key, PeriodID: 1, UserID: 987654321, ActionID: key, TriggerType: e.trigger, RewardDefinitionID: 1, RewardType: e.kind, Amount: e.amount, RandomValue: strings.Repeat("a", 64), ConfigVersion: "test", Status: e.status, SourceRef: key, Reason: "statistics test", BudgetType: "loyalty", CreatedAt: now, SettledAt: &now, ReversedAt: e.reversed}
		if err = tx.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var plan struct{ Key string }
	if err = tx.Raw("EXPLAIN SELECT user_id,SUM(amount) FROM pulse_ledger_entry FORCE INDEX (idx_pulse_ledger_user_asset_operation_amount) WHERE asset_type='ticket' AND user_id IN (?,?) GROUP BY user_id", 987654321, 987654322).Scan(&plan).Error; err != nil || plan.Key != "idx_pulse_ledger_user_asset_operation_amount" {
		t.Fatalf("statistics index: %+v %v", plan, err)
	}
	got, err := (&ticketRepository{db: tx}).Statistics(context.Background(), []uint64{987654321, 987654322, 987654323})
	if err != nil || got[987654321].Remaining != 9 || got[987654321].Total != 16 || got[987654322].Total != 0 || got[987654321].Spent != 3 || got[987654321].Removed != 4 || got[987654321].RewardQuota != 625000 || got[987654321].RewardExperience != 20 || got[987654323].Remaining != 0 || got[987654323].Total != 258912 || got[987654323].Spent != 1 || got[987654323].Removed != 258911 {
		t.Fatalf("%+v %v", got, err)
	}
	// Display reset preserves all ledger/balance/reward facts. The starting
	// total intentionally equals spent, even when usable tickets remain.
	if err = tx.Exec("INSERT INTO pulse_ticket_statistics_baseline(user_id,total_offset,removed_offset,original_total,original_spent,original_removed,corrected_at,reason) VALUES(987654321,13,4,16,3,4,NOW(),'test'),(987654323,258911,258911,258912,1,258911,NOW(),'test')").Error; err != nil {
		t.Fatal(err)
	}
	got, err = (&ticketRepository{db: tx}).Statistics(context.Background(), []uint64{987654321, 987654323})
	if err != nil || got[987654321].Total != 3 || got[987654321].Removed != 0 || got[987654321].Remaining != 9 || got[987654321].RewardQuota != 625000 || got[987654323].Total != 1 || got[987654323].Removed != 0 {
		t.Fatalf("reset: %+v %v", got, err)
	}
	for i, e := range []struct {
		op string
		n  int64
	}{{"ticket_bonus", 2}, {"ticket_spend", -1}, {"ticket_expire", -1}} {
		if err = tx.Exec("INSERT INTO pulse_ledger_entry(user_id,period_id,asset_type,operation,amount,balance_after,source_type,source_ref,idempotency_key,payload_hash,created_at) VALUES(987654323,1,'ticket',?,?,0,'statistics-new','fixture',?,REPEAT('c',64),NOW())", e.op, e.n, fmt.Sprintf("new-%d", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err = (&ticketRepository{db: tx}).Statistics(context.Background(), []uint64{987654323})
	if err != nil || got[987654323].Total != 3 || got[987654323].Spent != 2 || got[987654323].Removed != 1 || got[987654323].Remaining != 0 {
		t.Fatalf("after reset activity: %+v %v", got, err)
	}
}
