package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"sync"
	"testing"
	"time"
)

func TestMySQLContinuousTicketsAgeReplayAndRuleChanges(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, _ := mysqlstore.NewUnitOfWork(database)
	ctx := context.Background()
	now := time.Date(2088, 1, 1, 0, 0, 0, 0, time.UTC)
	prefix := fmt.Sprintf("continuous-%d", time.Now().UnixNano())
	var ids []uint64
	t.Cleanup(func() {
		for _, id := range ids {
			db.Exec("DELETE FROM pulse_period WHERE id=?", id)
		}
	})
	svc, _ := NewPeriodCreateService(unit, func() time.Time { return now })
	request := PeriodAdminRequest{Key: prefix, Continuous: true, QuotaValidityDays: 30, MultiplierBps: 10000, TicketThresholdMilli: 1000, RewardBudget: 10000, ExperienceBudget: 10000, Rewards: []PeriodRewardSpec{{Key: "quota", Amount: 10, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 5, Weight: 1}}, Reason: "test continuous rules"}
	first, err := svc.CreateFromAdmin(ctx, request, "test", prefix)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, first.PeriodID)
	now = now.Add(time.Second)
	replay, err := svc.CreateFromAdmin(ctx, request, "test", prefix)
	if err != nil || replay.PeriodID != first.PeriodID || !replay.StartsAt.Equal(first.StartsAt) {
		t.Fatalf("time-dependent replay: %+v %v", replay, err)
	}
	user := uint64(time.Now().UnixNano()%1000000000 + 1000000000)
	ingest := func(id string, amount int64, at time.Time) {
		t.Helper()
		event := usage.Event{SourceSystem: "new-api-log", SourceEventID: prefix + id, PayloadHash: fmt.Sprintf("%064x", amount), UserID: user, EventType: usage.EventConsume, SourceCreatedAt: at, QuotaDelta: amount, FundingProof: prefix + id, CursorValue: fmt.Sprintf("%d:1", at.Unix())}
		in, _ := NewUsageIngestService(unit, staticUsageSource{events: []usage.Event{event}}, UsageIngestConfig{CursorName: prefix + id, BatchSize: 1, TicketThresholdMilli: 1000})
		in.now = func() time.Time { return at }
		for i := 0; i < 100; i++ {
			if _, err := in.IngestBatch(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	ingest("one", 1500, now)
	request.ExpectedPeriodID = first.PeriodID
	request.Key = prefix + "-2"
	request.QuotaValidityDays = 10
	request.TicketThresholdMilli = 2000
	now = now.Add(time.Hour)
	second, err := svc.CreateFromAdmin(ctx, request, "test", request.Key)
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, second.PeriodID)
	stale := request
	stale.Key += "-stale"
	if _, err := svc.CreateFromAdmin(ctx, stale, "test", stale.Key); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("stale admin form accepted: %v", err)
	}
	ingest("two", 1500, now.Add(time.Second)) // 500 carried + 1500 = one new ticket.
	var lots, count int
	db.QueryRow("SELECT COUNT(*),SUM(issued) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&lots, &count)
	if lots != 2 || count != 2 {
		t.Fatalf("lost remainder/replay: %d/%d", lots, count)
	}
	rules := NewRewardRulesService(unit, true)
	rules.now = func() time.Time { return first.StartsAt.Add(30 * 24 * time.Hour) }
	before, err := rules.GetForUser(ctx, user)
	if err != nil || before.ExperienceOnly || len(before.Rewards) != 2 {
		t.Fatalf("ticket expired before deadline: %+v %v", before, err)
	}
	// Exhaust quota funding in this isolated fixture. A fresh ticket must pause
	// without spending; at expiry the same ticket only needs the EXP budget.
	if _, err := db.Exec("UPDATE pulse_reward_budget SET settled_amount=hard_cap WHERE period_id=? AND budget_type=?", first.PeriodID, ActionBudgetType); err != nil {
		t.Fatal(err)
	}
	paused, _ := NewActionService(unit, ActionConfig{RandomSecret: []byte("test-only-random"), RequireVerifiedFunding: true, Now: rules.now})
	_, err = paused.Execute(ctx, ActionCommand{UserID: user, ActionID: prefix + "-budget", IdempotencyKey: prefix + "-budget", TriggerType: ActionTriggerType})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("quota budget bypass: %v", err)
	}
	var unspent int
	if err := db.QueryRow("SELECT SUM(remaining) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&unspent); err != nil || unspent != 2 {
		t.Fatalf("budget failure spent tickets: %d %v", unspent, err)
	}
	rules.now = func() time.Time { return first.StartsAt.Add(30*24*time.Hour + time.Second) }
	view, err := rules.GetForUser(ctx, user)
	if err != nil || !view.Enabled || !view.ExperienceOnly || len(view.Rewards) != 1 || view.Rewards[0].RewardType != ExperienceRewardType || view.Period.ID != first.PeriodID {
		t.Fatalf("old ticket rule/expiry: %+v %v", view, err)
	}
	action, _ := NewActionService(unit, ActionConfig{RandomSecret: []byte("test-only-random"), RequireVerifiedFunding: true, Now: rules.now})
	command := ActionCommand{UserID: user, ActionID: prefix + "-draw", IdempotencyKey: prefix + "-draw", TriggerType: ActionTriggerType}
	var wg sync.WaitGroup
	results := make([]ActionResult, 100)
	failures := make([]error, 100)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], failures[i] = action.Execute(ctx, command) }(i)
	}
	wg.Wait()
	for i, e := range failures {
		if e != nil || results[i].RewardType != ExperienceRewardType || results[i].GrantID != results[0].GrantID {
			t.Fatalf("replay %d: %+v %v", i, results[i], e)
		}
	}
	var remaining, allocations int
	db.QueryRow("SELECT SUM(remaining) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&remaining)
	db.QueryRow("SELECT COUNT(*) FROM pulse_ticket_allocation a JOIN pulse_ticket_lot l ON l.id=a.lot_id WHERE l.user_id=?", user).Scan(&allocations)
	if remaining != 1 || allocations != 1 {
		t.Fatalf("double spend: %d/%d", remaining, allocations)
	}
	// Different action IDs racing for the last ticket: exactly one commits.
	lastErrors := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := command
			cmd.ActionID = fmt.Sprintf("%s-final-%d", prefix, i)
			cmd.IdempotencyKey = cmd.ActionID
			_, lastErrors[i] = action.Execute(ctx, cmd)
		}(i)
	}
	wg.Wait()
	successes := 0
	for _, e := range lastErrors {
		if e == nil {
			successes++
		} else if !errors.Is(e, ErrInsufficientTickets) {
			t.Fatal(e)
		}
	}
	if successes != 1 {
		t.Fatal("overspent tickets")
	}
	err = unit.Do(ctx, func(r ports.Repositories) error {
		pending, e := r.Tickets.PendingContribution(ctx, user)
		if e != nil {
			return e
		}
		if pending != 0 {
			t.Fatalf("pending=%d", pending)
		}
		accounts, e := r.Account.ListForUser(ctx, user)
		if e != nil {
			return e
		}
		for _, a := range accounts {
			entries, e := r.Ledger.ListAccountEntries(ctx, user, a.PeriodID, a.AssetType)
			if e != nil {
				return e
			}
			rebuilt, e := ledger.Rebuild(ledger.Account{UserID: user, PeriodID: a.PeriodID, AssetType: a.AssetType}, entries)
			if e != nil {
				return e
			}
			if rebuilt.Balance != a.Balance {
				t.Fatal("ledger mismatch")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
