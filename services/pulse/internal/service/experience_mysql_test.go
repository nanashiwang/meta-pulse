package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"sync"
	"testing"
	"time"
)

func TestMySQLExperienceDeliveryRecoveryAndReversal(t *testing.T) {
	database, db := openMySQLIntegration(t)
	ctx := context.Background()
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().AddDate(100, 0, 0).UTC().Truncate(time.Second)
	var last *time.Time
	if err = db.QueryRow("SELECT MAX(ends_at) FROM pulse_period").Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last != nil && !last.Before(start) {
		start = last.Add(time.Hour)
	}
	key := fmt.Sprintf("experience-%d", time.Now().UnixNano())
	creator, _ := NewPeriodCreateService(unit, time.Now)
	p, err := creator.CreateFromAdmin(ctx, PeriodAdminRequest{Key: key, StartsAt: start, MultiplierBps: 10000, TicketThresholdMilli: 1000, ExperienceBudget: 100000, Rewards: []PeriodRewardSpec{{Key: "community-500", RewardType: ExperienceRewardType, Amount: 500, Weight: 1}}, Reason: "社区经验可靠交付测试"}, "1", key)
	if err != nil {
		t.Fatal(err)
	}
	user := uint64(time.Now().UnixNano()%10000000 + 10000000)
	err = unit.Do(ctx, func(r ports.Repositories) error {
		_, err := appendEntry(ctx, r, ledger.Entry{UserID: user, PeriodID: p.PeriodID, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketMint, Amount: 40, SourceType: "integration", SourceRef: key, IdempotencyKey: key, PayloadHash: fmt.Sprintf("%064d", 1), Reason: "test tickets"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	action, _ := NewActionService(unit, ActionConfig{RandomSecret: []byte("experience-integration-random"), RequireVerifiedFunding: true, Now: func() time.Time { return start.Add(time.Hour) }})
	cmd := ActionCommand{UserID: user, ActionID: key, TriggerType: ActionTriggerType, IdempotencyKey: key}
	results := make([]ActionResult, 100)
	errs := make([]error, 100)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = action.Execute(ctx, cmd) }(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil || results[i].GrantID != results[0].GrantID {
			t.Fatalf("action %d: %v", i, e)
		}
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM pulse_settlement_outbox o JOIN pulse_reward_grant g ON g.id=o.reward_grant_id WHERE g.user_id=? AND o.status IN ('pending','retry','processing')", user).Scan(&count); err != nil || count != 0 {
		t.Fatalf("monetary worker can lease experience %d %v", count, err)
	}
	delivery := NewExperienceService(unit)
	pending, err := delivery.Pending(ctx, user)
	if err != nil || len(pending) != 1 || pending[0].Amount != 500 {
		t.Fatalf("delivery %+v %v", pending, err)
	}
	if err = delivery.Acknowledge(ctx, user+1, results[0].GrantID, "pending"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("identity", err)
	}
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = delivery.Acknowledge(ctx, user, results[0].GrantID, "pending") }(i)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var reserved, settled, released int64
	check := func(wr, ws, wl int64) {
		t.Helper()
		if err = db.QueryRow("SELECT reserved_amount,settled_amount,released_amount FROM pulse_reward_budget WHERE period_id=? AND budget_type='community_exp'", p.PeriodID).Scan(&reserved, &settled, &released); err != nil {
			t.Fatal(err)
		}
		if reserved != wr || settled != ws || released != wl {
			t.Fatalf("budget %d %d %d want %d %d %d", reserved, settled, released, wr, ws, wl)
		}
	}
	check(0, 500, 0)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = delivery.Reverse(ctx, "1", key, results[0].GrantID, "误发回收")
		}(i)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	check(0, 0, 500)
	if err = delivery.Acknowledge(ctx, user, results[0].GrantID, "pending"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("stale ack", err)
	}
	pending, err = delivery.Pending(ctx, user)
	if err != nil || len(pending) != 1 || pending[0].Status != "reversed" {
		t.Fatalf("reversal delivery %+v %v", pending, err)
	}
	if err = delivery.Acknowledge(ctx, user, results[0].GrantID, "reversed"); err != nil {
		t.Fatal(err)
	}
	// More than the ordinary history window must drain without a cursor gap.
	for i := 0; i < 25; i++ {
		cmd.ActionID = fmt.Sprintf("%s-%d", key, i)
		cmd.IdempotencyKey = cmd.ActionID
		if _, err = action.Execute(ctx, cmd); err != nil {
			t.Fatal(err)
		}
	}
	drained := 0
	for {
		rows, e := delivery.Pending(ctx, user)
		if e != nil {
			t.Fatal(e)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if e = delivery.Acknowledge(ctx, user, row.GrantID, row.Status); e != nil {
				t.Fatal(e)
			}
			drained++
		}
	}
	if drained != 25 {
		t.Fatal("skipped deliveries", drained)
	}
	check(0, 12500, 500)
	// Cancellation before delivery creates a reversal, never an award.
	cmd.ActionID = key + "-cancel"
	cmd.IdempotencyKey = cmd.ActionID
	cancelled, e := action.Execute(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	if e = delivery.Reverse(ctx, "1", key+"-cancel", cancelled.GrantID, "取消待发经验"); e != nil {
		t.Fatal(e)
	}
	check(0, 12500, 1000)
	rows, e := delivery.Pending(ctx, user)
	if e != nil || len(rows) != 1 || rows[0].Status != "reversed" {
		t.Fatalf("cancel %+v %v", rows, e)
	}
	// EXP is not included in financial budget aggregates.
	var moneyCap int64
	if e = db.QueryRow("SELECT COALESCE(SUM(hard_cap),0) FROM pulse_reward_budget WHERE period_id=? AND budget_type <> 'community_exp'", p.PeriodID).Scan(&moneyCap); e != nil || moneyCap != 0 {
		t.Fatal("mixed units", moneyCap, e)
	}
}
