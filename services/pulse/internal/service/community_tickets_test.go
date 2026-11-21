package service

import (
	"context"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
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
	now := time.Date(2077, 1, 4, 8, 0, 0, 0, time.UTC)
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
	task := CommunityTicket{Day: now.In(communityZone).Format("2006-01-02"), Category: "checkin_weekly", Ordinal: 1}
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
	for _, category := range []string{"received", "participation", "checkin_weekly"} {
		for i := 1; i <= map[string]int{"received": 2, "participation": 3, "checkin_weekly": 3}[category]; i++ {
			_, err = s.Grant(ctx, user, CommunityTicket{task.Day, category, i})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, bad := range []CommunityTicket{{task.Day, "checkin_weekly", 4}, {task.Day, "received", 3}, {task.Day, "participation", 4}, {task.Day, "like", 1}, {"2077-02-30", "checkin", 1}, {"2078-01-01", "checkin", 1}, {task.Day, "checkin", 0}} {
		if _, e := s.Grant(ctx, user, bad); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	var count, total int
	if err = db.QueryRow("SELECT COUNT(*),SUM(issued) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 8 || total != 8 {
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
	if err = db.QueryRow("SELECT COUNT(*),SUM(issued) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 8 || total != 8 {
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
	if e != nil || account.Balance != 7 {
		t.Fatalf("ledger %+v %v", account, e)
	}
	var contributions int
	db.QueryRow("SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND asset_type='contribution'", user).Scan(&contributions)
	if contributions != 1 {
		t.Fatal("usage contribution replay or community isolation failed")
	}
}

func TestAdminTicketGrantRejectsInvalidInputBeforeWriting(t *testing.T) {
	s := NewCommunityTicketService(nil)
	for _, g := range []AdminTicketGrant{{UserID: 0, Count: 1, Reason: "验证"}, {UserID: 1, Count: 0, Reason: "验证"}, {UserID: 1, Count: MaxAdminTicketGrant + 1, Reason: "验证"}, {UserID: 1, Count: 1, Reason: " "}} {
		if _, err := s.AdminGrant(context.Background(), "9", "k", g); err != ErrInvalidAdminTicketGrant {
			t.Fatalf("%+v: %v", g, err)
		}
	}
	if _, err := s.AdminGrant(context.Background(), "", "k", AdminTicketGrant{UserID: 1, Count: 1, Reason: "验证"}); err != ErrInvalidAdminTicketGrant {
		t.Fatal("missing actor accepted")
	}
}

func TestMySQLAdminTicketGrantReplayMintsOneLot(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, _ := mysqlstore.NewUnitOfWork(database)
	ctx := context.Background()
	now := time.Date(2078, 1, 1, 8, 0, 0, 0, time.UTC)
	key := fmt.Sprintf("admin-grant-%d", time.Now().UnixNano())
	user := uint64(time.Now().UnixNano()%1000000000 + 3000000000)
	creator, _ := NewPeriodCreateService(unit, func() time.Time { return now })
	p, err := creator.CreateFromAdmin(ctx, PeriodAdminRequest{Key: key, Continuous: true, QuotaValidityDays: 30, QuotaBudgetUnlimited: true, ExperienceBudgetUnlimited: true, MultiplierBps: 10000, TicketThresholdMilli: 1000, Rewards: []PeriodRewardSpec{{Key: "quota", Amount: 1, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 1, Weight: 1}}, Reason: "管理员赠券测试"}, "test", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", p.PeriodID) })
	now = now.Add(time.Second)
	s := NewCommunityTicketService(unit)
	s.now = func() time.Time { return now }
	grant := AdminTicketGrant{UserID: user, Count: 10, Reason: "验证新概率"}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.AdminGrant(ctx, "9", key, grant); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count, total int
	if err = db.QueryRow("SELECT COUNT(*),SUM(remaining) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 1 || total != 10 {
		t.Fatalf("lots %d %d %v", count, total, err)
	}
	var balance int64
	if err = db.QueryRow("SELECT COALESCE(SUM(amount),0) FROM pulse_ledger_entry WHERE user_id=? AND asset_type=?", user, ledger.AssetTicket).Scan(&balance); err != nil || balance != 10 {
		t.Fatalf("ledger %d %v", balance, err)
	}
	if _, err = s.AdminGrant(ctx, "9", key, AdminTicketGrant{UserID: user, Count: 11, Reason: "验证新概率"}); err == nil {
		t.Fatal("same key with different payload accepted")
	}
}

func TestCommunityTicketScheduleValidation(t *testing.T) {
	for _, tc := range []struct {
		day, category string
		want          int
	}{
		{"2026-11-20", "checkin_weekly", 3}, {"2026-11-23", "checkin_weekly", 3},
		{"2026-11-19", "checkin_weekly", 0}, {"2026-11-21", "checkin_weekly", 0},
		{"2026-11-16", "checkin_weekly", 0}, {"2026-11-17", "checkin", 1}, {"2026-11-18", "checkin", 1},
	} {
		day, err := time.ParseInLocation("2006-01-02", tc.day, communityZone)
		if err != nil {
			t.Fatal(err)
		}
		if got := communityTicketCap(CommunityTicket{Day: tc.day, Category: tc.category}, day); got != tc.want {
			t.Fatal(tc, got)
		}
	}
	s := NewCommunityTicketService(nil)
	s.now = func() time.Time { return time.Date(2026, 11, 24, 0, 0, 0, 0, communityZone) }
	for _, task := range []CommunityTicket{{"2026-11-19", "checkin_weekly", 1}, {"2026-11-20", "checkin_weekly", 4}, {"2026-11-20", "checkin_weekly", 0}} {
		if _, err := s.Grant(context.Background(), 42, task); err == nil {
			t.Fatal("invalid schedule accepted", task)
		}
	}
}

func TestLegacyDailyCheckinBeyondActualPolicyCutoverIsRejected(t *testing.T) {
	u := newPeriodCreateUnit()
	u.admin.periods = append(u.admin.periods, period.Period{ID: 1, Key: CheckinRewardPolicyKey, Status: period.StatusActive, StartsAt: time.Date(2026, 11, 20, 0, 0, 0, 0, communityZone)})
	s := NewCommunityTicketService(u)
	s.now = func() time.Time { return time.Date(2026, 11, 24, 0, 0, 0, 0, communityZone) }
	if _, err := s.Grant(context.Background(), 42, CommunityTicket{Day: "2026-11-21", Category: "checkin", Ordinal: 1}); err != ports.ErrConflict {
		t.Fatal(err)
	}
}

func TestMySQLFarmExchangeReplayMintsOneLot(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, _ := mysqlstore.NewUnitOfWork(database)
	ctx := context.Background()
	now := time.Date(2081, 1, 1, 8, 0, 0, 0, time.UTC)
	key := fmt.Sprintf("farm-grant-%d", time.Now().UnixNano())
	user := uint64(time.Now().UnixNano()%1000000000 + 3000000000)
	creator, _ := NewPeriodCreateService(unit, func() time.Time { return now })
	p, err := creator.CreateFromAdmin(ctx, PeriodAdminRequest{Key: key, Continuous: true, QuotaValidityDays: 30, QuotaBudgetUnlimited: true, ExperienceBudgetUnlimited: true, MultiplierBps: 10000, TicketThresholdMilli: 1000, Rewards: []PeriodRewardSpec{{Key: "quota", Amount: 1, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 1, Weight: 1}}, Reason: "农场兑换测试"}, "test", key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", p.PeriodID) })
	now = now.Add(time.Second)
	s := NewCommunityTicketService(unit)
	s.now = func() time.Time { return now }
	grant := FarmTicketExchange{OrderID: fmt.Sprintf("%032x", time.Now().UnixNano())}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ExchangeFarmTicket(ctx, user, grant); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count, total int
	if err = db.QueryRow("SELECT COUNT(*),SUM(remaining) FROM pulse_ticket_lot WHERE user_id=?", user).Scan(&count, &total); err != nil || count != 1 || total != 1 {
		t.Fatalf("lots %d %d %v", count, total, err)
	}
	var balance int64
	if err = db.QueryRow("SELECT COALESCE(SUM(amount),0) FROM pulse_ledger_entry WHERE user_id=? AND asset_type=?", user, ledger.AssetTicket).Scan(&balance); err != nil || balance != 1 {
		t.Fatalf("ledger %d %v", balance, err)
	}
	if _, err = s.ExchangeFarmTicket(ctx, user+1, grant); err == nil {
		t.Fatal("same key with different payload accepted")
	}
}
