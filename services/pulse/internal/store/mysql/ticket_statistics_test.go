package mysql

import (
	"context"
	"github.com/nanashiwang/meta-pulse/migrations"
	"os"
	"testing"
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
	if _, err = db.SQL().Exec("DELETE FROM goose_db_version WHERE version_id=20"); err != nil {
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
	var plan struct{ Key string }
	if err = tx.Raw("EXPLAIN SELECT user_id,SUM(amount) FROM pulse_ledger_entry FORCE INDEX (idx_pulse_ledger_user_asset_operation_amount) WHERE asset_type='ticket' AND user_id IN (?,?) GROUP BY user_id", 987654321, 987654322).Scan(&plan).Error; err != nil || plan.Key != "idx_pulse_ledger_user_asset_operation_amount" {
		t.Fatalf("statistics index: %+v %v", plan, err)
	}
	got, err := (&ticketRepository{db: tx}).Statistics(context.Background(), []uint64{987654321, 987654322})
	if err != nil || got[987654321].Remaining != 9 || got[987654321].Total != 16 || got[987654322].Total != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
