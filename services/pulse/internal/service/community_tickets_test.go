package service

import (
	"context"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"sync"
	"testing"
	"time"
)

func TestMySQLCommunityTicketsReplayCapsDrawAndContribution(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, _ := mysqlstore.NewUnitOfWork(database)
	ctx := context.Background()
	now := time.Date(2077, 1, 1, 8, 0, 0, 0, time.UTC)
	key := fmt.Sprintf("community-%d", time.Now().UnixNano())
	user := uint64(time.Now().UnixNano()%1000000000 + 2000000000)
	creator, _ := NewPeriodCreateService(unit, func() time.Time { return now })
	request := PeriodAdminRequest{Key: key, Continuous: true, QuotaValidityDays: 30, MultiplierBps: 10000, TicketThresholdMilli: 1000, RewardBudget: 10000, ExperienceBudget: 10000, Rewards: []PeriodRewardSpec{{Key: "quota", Amount: 1, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 1, Weight: 1}}, Reason: "社区赠券测试"}
	p, err := creator.CreateFromAdmin(ctx, request, "test", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", p.PeriodID) })
	now = now.Add(time.Second)
	s := NewCommunityTicketService(unit)
	s.now = func() time.Time { return now }
	task := CommunityTicket{Day: now.In(communityZone).Format("2006-01-02"), Category: "checkin", Ordinal: 1}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Grant(ctx, user, task); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, category := range []string{"received", "participation"} {
		for i := 1; i <= map[string]int{"received": 2, "participation": 3}[category]; i++ {
			_, err = s.Grant(ctx, user, CommunityTicket{task.Day, category, i})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, bad := range []CommunityTicket{{task.Day, "checkin", 2}, {task.Day, "received", 3}, {task.Day, "participation", 4}, {task.Day, "like", 1}, {"2077-02-30", "checkin", 1}, {"2078-01-01", "checkin", 1}, {task.Day, "checkin", 0}} {
		if _, e := s.Grant(ctx, user, bad); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	var count, total int
	if err = db.QueryRow("SELECT COUNT(*),SUM(issued) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 6 || total != 6 {
		t.Fatalf("lots %d %d %v", count, total, err)
	}
	if err = unit.Do(ctx, func(r ports.Repositories) error {
		n, e := r.Tickets.PendingContribution(ctx, user)
		if e != nil {
			return e
		}
		if n != 0 {
			t.Errorf("bonus consumed contribution %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// API spending after community issuance must not add tickets or alter lots.
	event := usage.Event{SourceSystem: "new-api-log", SourceEventID: key, CursorValue: "1:1", PayloadHash: fmt.Sprintf("%064x", 1), FundingProof: key, UserID: user, EventType: usage.EventConsume, SourceCreatedAt: now, QuotaDelta: 1_000_000}
	ingest, _ := NewUsageIngestService(unit, staticUsageSource{events: []usage.Event{event}}, UsageIngestConfig{CursorName: key, BatchSize: 1})
	for i := 0; i < 100; i++ {
		result, e := ingest.IngestBatch(ctx)
		if e != nil || result.TicketsMinted != 0 || result.TicketsReversed != 0 {
			t.Fatalf("usage: %+v %v", result, e)
		}
	}
	if err = db.QueryRow("SELECT COUNT(*),SUM(issued) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 6 || total != 6 {
		t.Fatalf("usage altered bonus lots: %d %d %v", count, total, err)
	}
	// Changing the latest rule and date must not remint an old receipt or change
	// its first issuance/expiry, including a lost successful HTTP response.
	first, _ := s.Grant(ctx, user, task)
	now = now.Add(24 * time.Hour)
	request.Key += "-next"
	request.ExpectedPeriodID = p.PeriodID
	next, e := creator.CreateFromAdmin(ctx, request, "test", request.Key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", next.PeriodID) })
	replay, e := s.Grant(ctx, user, task)
	if e != nil || replay != first {
		t.Fatalf("replay %+v %v", replay, e)
	}
	rules := NewRewardRulesService(unit, true)
	rules.SelectionSecret = []byte("community-test-secret")
	rules.now = s.now
	choice, e := firstRuleGroup(rules, ctx, user)
	if e != nil {
		t.Fatal(e)
	}
	action, _ := NewActionService(unit, ActionConfig{RandomSecret: rules.SelectionSecret, RequireVerifiedFunding: true, Now: s.now})
	_, e = action.Execute(ctx, ActionCommand{UserID: user, ActionID: key, IdempotencyKey: key, ProtocolVersion: 3, Selection: choice.Selection, TriggerType: ActionTriggerType})
	if e != nil {
		t.Fatal(e)
	}
	ledgerService, _ := NewLedgerService(unit)
	account, e := ledgerService.RebuildAccount(ctx, user, p.PeriodID, ledger.AssetTicket)
	if e != nil || account.Balance != 5 {
		t.Fatalf("ledger %+v %v", account, e)
	}
	var contributions int
	db.QueryRow("SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND asset_type='contribution'", user).Scan(&contributions)
	if contributions != 1 {
		t.Fatal("usage contribution replay or community isolation failed")
	}
}
