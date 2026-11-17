package mysql

import (
	"context"
	"fmt"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/migrations"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMySQLRewardHistoryReadsOutboxWithoutMutatingGrant(t *testing.T) {
	dsn := os.Getenv("PULSE_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("set PULSE_INTEGRATION_DSN to an isolated disposable schema")
	}
	parsed, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid integration DSN")
	}
	if !(strings.HasSuffix(parsed.DBName, "_test") || strings.HasSuffix(parsed.DBName, "_integration")) {
		t.Fatal("disposable schema required")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err = migrations.Up(ctx, db.SQL()); err != nil {
		t.Fatal(err)
	}
	tx := db.GORM().WithContext(ctx).Begin()
	defer tx.Rollback()
	if err = tx.Exec("DELETE FROM pulse_reward_grant").Error; err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	for i, status := range []string{"dead", "conflict", "retry", "completed"} {
		key := fmt.Sprintf("blocked-test-%d", i)
		g := rewardGrantModel{GrantID: key, PeriodID: 1, UserID: 7, ActionID: key, TriggerType: "pulse", RewardDefinitionID: 1, RewardType: "newapi_quota", Amount: 500000, RandomValue: strings.Repeat("a", 64), ConfigVersion: "test", Status: "pending", SourceRef: key, Reason: "test", BudgetType: "loyalty", CreatedAt: now}
		if err := tx.Create(&g).Error; err != nil {
			t.Fatal(err)
		}
		if err := tx.Exec("INSERT INTO pulse_settlement_outbox (reward_grant_id,payload_hash,payload_json,status) VALUES (?, ?, '{}', ?)", g.ID, strings.Repeat("a", 64), status).Error; err != nil {
			t.Fatal(err)
		}
		repo := &rewardRepository{db: tx}
		rows, err := repo.ListPulseGrantsByAction(ctx, 7, key)
		if err != nil || len(rows) != 1 || rows[0].SettlementStatus != status || rows[0].Status != "pending" {
			t.Fatalf("action rows=%+v err=%v", rows, err)
		}
		var saved rewardGrantModel
		if err := tx.First(&saved, g.ID).Error; err != nil || saved.Status != "pending" {
			t.Fatal("history changed grant", err)
		}
	}
	repo := &rewardRepository{db: tx}
	rows, err := repo.ListGrantsForUser(ctx, 7, 100)
	if err != nil || len(rows) != 4 {
		t.Fatalf("history=%+v err=%v", rows, err)
	}
	for _, row := range rows {
		if row.SettlementStatus == "" {
			t.Fatal("missing settlement status")
		}
	}
	rows, err = repo.ListGrantsForUser(ctx, 8, 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("cross-user history leak", err)
	}
	if err := repo.loadHistorySettlementStatuses(ctx, []ports.RewardGrant{}); err != nil {
		t.Fatal(err)
	}
}
