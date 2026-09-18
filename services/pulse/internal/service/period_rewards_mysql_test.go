package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func TestMySQLRewardPeriodSetupAtomicAndFrozen(t *testing.T) {
	database, db := openMySQLIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewPeriodCreateService(unit, time.Now)
	c := fundedPeriodCommand()
	c.Key = fmt.Sprintf("integration-reward-setup-%d", time.Now().UnixNano())
	c.StartsAt = time.Now().AddDate(40, 0, 0).Truncate(time.Second)
	var previous sql.NullTime
	if err := db.QueryRowContext(ctx, "SELECT MAX(ends_at) FROM pulse_period WHERE period_key LIKE 'integration-reward-setup-%'").Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if previous.Valid && !previous.Time.Before(c.StartsAt) {
		c.StartsAt = previous.Time.Add(time.Hour)
	}
	result, err := svc.Create(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if result.FundingPolicy != period.VerifiedPaidFunding || result.Status != "active" {
		t.Fatalf("result=%+v", result)
	}
	var definitions, budgets, audits int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_reward_definition WHERE period_id=?", result.PeriodID).Scan(&definitions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_reward_budget WHERE period_id=? AND hard_cap=100", result.PeriodID).Scan(&budgets); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_audit_log WHERE resource_id=? AND action='period_create'", c.Key).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if definitions != 2 || budgets != 1 || audits != 1 {
		t.Fatalf("definitions=%d budgets=%d audits=%d", definitions, budgets, audits)
	}
	for _, query := range []string{
		"UPDATE pulse_reward_definition SET amount=amount+1 WHERE period_id=?",
		"UPDATE pulse_reward_definition SET weight=weight+1 WHERE period_id=?",
		"UPDATE pulse_reward_definition SET enabled=0 WHERE period_id=?",
		"UPDATE pulse_reward_definition SET transferable_quota=1 WHERE period_id=?",
		"DELETE FROM pulse_reward_definition WHERE period_id=?",
		"INSERT INTO pulse_reward_definition(period_id,reward_key,reward_type,amount,weight,config_version,enabled) VALUES(?,'extra','newapi_quota',1,1,'v1',1)",
		"UPDATE pulse_reward_budget SET hard_cap=1000 WHERE period_id=?",
		"UPDATE pulse_reward_budget SET budget_type='content_reward' WHERE period_id=?",
		"DELETE FROM pulse_reward_budget WHERE period_id=?",
		"INSERT INTO pulse_reward_budget(period_id,budget_type,hard_cap) VALUES(?,'content_reward',100)",
		"UPDATE pulse_period SET ticket_threshold_milli=2000 WHERE id=?",
		"UPDATE pulse_period SET funding_policy='legacy' WHERE id=?",
	} {
		if _, err := db.ExecContext(ctx, query, result.PeriodID); err == nil {
			t.Fatalf("active configuration mutated: %s", query)
		}
	}
	if _, err := db.ExecContext(ctx, "UPDATE pulse_reward_budget SET reserved_amount=5,settled_amount=2,released_amount=1,version=1 WHERE period_id=?", result.PeriodID); err != nil {
		t.Fatalf("runtime counters frozen: %v", err)
	}
	if err := unit.Do(ctx, func(repos ports.Repositories) error {
		_, err := repos.RewardAdmin.CreateDefinition(ctx, result.PeriodID, reward.Definition{RewardKey: "extra", RewardType: "newapi_quota", Amount: 1, Weight: 1, Enabled: true, ConfigVersion: c.ConfigVersion})
		return err
	}); err == nil {
		t.Fatal("repository extended active pool")
	}
	// Fail after every economic row was written: audit validation must roll
	// back the activation, rules, definitions and budget together.
	counts := func() []int {
		var out []int
		for _, table := range []string{"pulse_period", "pulse_economics_rule", "pulse_reward_definition", "pulse_reward_budget", "pulse_audit_log"} {
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		return out
	}
	before := counts()
	failed := c
	failed.Key = c.Key + "-fail"
	failed.StartsAt = result.EndsAt
	failed.Reason = strings.Repeat("x", 501)
	if _, err := svc.Create(ctx, failed); err == nil {
		t.Fatal("audit write failure accepted")
	}
	after := counts()
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("partial transaction: before=%v after=%v", before, after)
		}
	}
}
