package mysql

import (
	"context"
	"fmt"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/migrations"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMySQLRewardFeedFiltersAndStableOrder(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Second)
	sequence := 0
	insert := func(amount int64, status, trigger, kind string, created time.Time, reversed *time.Time) {
		t.Helper()
		sequence++
		key := fmt.Sprintf("private-feed-%d", sequence)
		row := rewardGrantModel{GrantID: key, PeriodID: 9, UserID: uint64(sequence), ActionID: key, TriggerType: trigger, RewardDefinitionID: 1, RewardType: kind, Amount: amount, RandomValue: strings.Repeat("x", 64), ConfigVersion: "private-config", Status: status, SourceRef: key, Reason: "private-reason", BudgetType: "loyalty", CreatedAt: created, SettledAt: &now, ReversedAt: reversed}
		if err := tx.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"pending", "shadow", "reversed", "reversal_pending", "settlement_dead", "conflict"} {
		insert(999, status, "pulse", "newapi_quota", now, nil)
	}
	insert(999, "settled", "content", "newapi_quota", now, nil)
	insert(999, "settled", "period", "community_exp", now, nil)
	insert(999, "settled", "pulse", "cash", now, nil)
	insert(0, "settled", "pulse", "community_exp", now, nil)
	insert(1<<53, "settled", "pulse", "newapi_quota", now, nil)
	insert(999, "settled", "pulse", "newapi_quota", now.Add(-31*24*time.Hour), nil)
	insert(999, "settled", "pulse", "newapi_quota", now.Add(time.Second), nil)
	insert(999, "settled", "pulse", "newapi_quota", now, &now)
	for i := 0; i < 25; i++ {
		insert(int64(i+1), "settled", "pulse", "community_exp", now.Add(-time.Duration(i+1)*time.Minute), nil)
	}
	insert(100, "settled", "pulse", "community_exp", now, nil)
	insert(625000, "settled", "pulse", "newapi_quota", now, nil)
	repo := &rewardFeedRepository{db: tx}
	rows, err := repo.ListSettledDraws(ctx, now.Add(-30*24*time.Hour), now, 20)
	if err != nil || len(rows) != 20 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	if rows[0].Amount != 625000 || rows[1].Amount != 100 || rows[2].Amount != 1 || rows[19].Amount != 18 {
		t.Fatalf("incorrect order/filter: %+v", rows)
	}
	if err = tx.Model(&rewardGrantModel{}).Where("grant_id = ?", fmt.Sprintf("private-feed-%d", sequence)).Update("status", "reversed").Error; err != nil {
		t.Fatal(err)
	}
	rows, err = repo.ListSettledDraws(ctx, now.Add(-30*24*time.Hour), now, 20)
	if err != nil || rows[0].Amount != 100 {
		t.Fatal("reversed reward still visible", err)
	}
	var count int64
	if err = tx.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name='pulse_reward_grant' AND index_name='idx_reward_feed'").Scan(&count).Error; err != nil || count != 4 {
		t.Fatal("missing feed index", count, err)
	}
}
