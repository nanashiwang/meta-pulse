package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func TestMySQLCurrentRuleAcrossTicketSourcesAndReplay(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, _ := mysqlstore.NewUnitOfWork(database)
	ctx := context.Background()
	base := time.Date(2095, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	secret := []byte("selection-test-only")
	key := fmt.Sprintf("selection-%d", time.Now().UnixNano())
	user := uint64(time.Now().UnixNano()%1000000000 + 3000000000)
	creator, _ := NewPeriodCreateService(unit, func() time.Time { return now })
	request := PeriodAdminRequest{Key: key, Continuous: true, QuotaValidityDays: 30, MultiplierBps: 10000, TicketThresholdMilli: 1000, RewardBudget: 1000, ExperienceBudget: 1000, Rewards: []PeriodRewardSpec{{Key: "quota", Amount: 10, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 10, Weight: 1}}, Reason: "selection regression"}
	old, e := creator.CreateFromAdmin(ctx, request, "test", key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", old.PeriodID) })
	mint := func(suffix string, amount int64) {
		t.Helper()
		event := usage.Event{SourceSystem: "new-api-log", SourceEventID: key + suffix, PayloadHash: fmt.Sprintf("%064x", amount), UserID: user, EventType: usage.EventConsume, SourceCreatedAt: now, QuotaDelta: amount, FundingProof: key + suffix, CursorValue: fmt.Sprintf("%d:1", now.Unix())}
		ingest, _ := NewUsageIngestService(unit, staticUsageSource{events: []usage.Event{event}}, UsageIngestConfig{CursorName: key + suffix, BatchSize: 1, TicketThresholdMilli: 1000})
		ingest.now = func() time.Time { return now }
		if _, err := ingest.IngestBatch(ctx); err != nil {
			t.Fatal(err)
		}
	}
	mint("-old-exp", 2000)
	now = base.Add(10 * 24 * time.Hour)
	mint("-old-mixed", 2000)
	now = base.Add(20 * 24 * time.Hour)
	request.Key = key + "-new"
	request.ExpectedPeriodID = old.PeriodID
	request.QuotaValidityDays = 60
	request.Rewards = []PeriodRewardSpec{{Key: "quota", Amount: 20, Weight: 1}, {Key: "exp", RewardType: ExperienceRewardType, Amount: 30, Weight: 3}}
	fresh, e := creator.CreateFromAdmin(ctx, request, "test", request.Key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", fresh.PeriodID) })
	mint("-new", 2000)
	now = now.Add(time.Hour)
	mint("-new-later", 1000)
	now = base.Add(31 * 24 * time.Hour)
	if _, e = db.Exec("UPDATE pulse_reward_budget SET settled_amount=hard_cap WHERE period_id=? AND budget_type='community_exp'", old.PeriodID); e != nil {
		t.Fatal(e)
	}
	rules := NewRewardRulesService(unit, true)
	rules.SelectionSecret = secret
	rules.now = func() time.Time { return now }

	view, e := rules.GetForUser(ctx, user)
	if e != nil || !view.Enabled || !view.CanDrawFive || len(view.Draws) != 5 || view.TicketCount != 7 || view.Period.ID != fresh.PeriodID || view.TotalWeight != 4 {
		t.Fatalf("unified view: %+v %v", view, e)
	}
	action, _ := NewActionService(unit, ActionConfig{RandomSecret: secret, RequireVerifiedFunding: true, Now: func() time.Time { return now }})
	command := func(id, selection string) ActionCommand {
		return ActionCommand{ProtocolVersion: 3, Selection: selection, UserID: user, ActionID: key + id, IdempotencyKey: key + id, TriggerType: ActionTriggerType}
	}
	// A wait crossing the original deadline still rejects before financial writes.
	clockCalls := 0
	boundary, _ := NewActionService(unit, ActionConfig{RandomSecret: secret, Now: func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return now
		}
		return view.Draws[0].QuotaExpiresAt
	}})
	if _, e = boundary.Execute(ctx, command("-boundary", view.Selection)); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("deadline: %v", e)
	}
	foreign := command("-foreign", view.Selection)
	foreign.UserID++
	if _, e = action.Execute(ctx, foreign); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("foreign: %v", e)
	}
	if _, e = action.Execute(ctx, command("-tamper", view.Selection+"a")); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("tamper: %v", e)
	}
	for _, version := range []int{0, 2} {
		legacy := command(fmt.Sprintf("-unsubmitted-v%d", version), view.Selection)
		legacy.ProtocolVersion = version
		if version == 0 {
			legacy.Selection = ""
		}
		if _, e = action.Execute(ctx, legacy); !errors.Is(e, ErrSelectionRequired) {
			t.Fatalf("legacy reinterpreted: %v", e)
		}
	}
	// One five-draw plan spans two source rules and three issuance lots, all at
	// the latest odds. Exhausted old budgets must never block these old tickets.
	var first ActionResult
	firstCmd := command("-draw-0", view.Draws[0].Selection)
	sourceIDs := map[uint64]bool{}
	for i, draw := range view.Draws {
		choice, err := verifySelection(secret, user, draw.Selection)
		if err != nil {
			t.Fatal(err)
		}
		sourceIDs[choice.SourcePeriodID] = true
		got, err := action.Execute(ctx, command(fmt.Sprintf("-draw-%d", i), draw.Selection))
		if err != nil || got.PeriodID != fresh.PeriodID || (got.Amount != 20 && got.Amount != 30) {
			t.Fatalf("latest rule draw: %+v %v", got, err)
		}
		if i == 0 {
			first = got
		}
	}
	if len(sourceIDs) != 2 {
		t.Fatal("five draw did not span source rules")
	}
	var oldReserved, newReserved int64
	db.QueryRow("SELECT SUM(reserved_amount) FROM pulse_reward_budget WHERE period_id=?", old.PeriodID).Scan(&oldReserved)
	db.QueryRow("SELECT SUM(reserved_amount) FROM pulse_reward_budget WHERE period_id=?", fresh.PeriodID).Scan(&newReserved)
	if oldReserved != 0 || newReserved < 100 {
		t.Fatalf("budget ownership %d/%d", oldReserved, newReserved)
	}
	// New 60-day settings cannot revive tickets originally issued for 30 days.
	expired, e := rules.GetForUser(ctx, user)
	if e != nil || !expired.ExperienceOnly || expired.TicketCount != 2 || len(expired.Rewards) != 1 || expired.Rewards[0].Amount != 30 || !expired.QuotaExpiresAt.Equal(base.Add(30*24*time.Hour)) {
		t.Fatalf("original validity: %+v %v", expired, e)
	}
	changed := firstCmd
	changed.Selection = expired.Selection
	if _, e = action.Execute(ctx, changed); !errors.Is(e, ledger.ErrIdempotencyConflict) {
		t.Fatalf("selection fingerprint %v", e)
	}
	changed = firstCmd
	changed.ProtocolVersion = 2
	if _, e = action.Execute(ctx, changed); !errors.Is(e, ledger.ErrIdempotencyConflict) {
		t.Fatalf("protocol fingerprint %v", e)
	}
	// Publishing a rule invalidates an unsubmitted preview, not committed results.
	now = now.Add(time.Second)
	request.Key = key + "-third"
	request.ExpectedPeriodID = fresh.PeriodID
	request.Rewards[1].Amount = 40
	// Pause after legacy reads established a snapshot, before the publication
	// mutex. The creator commits while this request is in flight.
	ready, resume := make(chan struct{}), make(chan struct{})
	gated, _ := NewActionService(gatedRuleUnit{unit, ready, resume}, ActionConfig{RandomSecret: secret, Now: func() time.Time { return now }})
	inFlight := make(chan error, 1)
	go func() { _, err := gated.Execute(ctx, command("-in-flight", expired.Selection)); inFlight <- err }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("action did not reach rule mutex")
	}
	newest, e := creator.CreateFromAdmin(ctx, request, "test", request.Key)
	if e != nil {
		t.Fatal(e)
	}
	close(resume)
	if err := <-inFlight; !errors.Is(err, ErrSelectionChanged) {
		t.Fatalf("stale transaction snapshot used old rule: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM pulse_period WHERE id=?", newest.PeriodID) })
	stale := command("-stale", expired.Selection)
	if _, e = action.Execute(ctx, stale); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("stale current rule: %v", e)
	}
	action.secret = []byte("rotated")
	action.cfg.DisableNewActions = true
	for i := 0; i < 100; i++ {
		got, err := action.Execute(ctx, firstCmd)
		if err != nil || got != first {
			t.Fatalf("committed replay: %+v %v", got, err)
		}
	}
	action.secret = secret
	action.cfg.DisableNewActions = false
	current, e := rules.GetForUser(ctx, user)
	if e != nil || current.Rewards[0].Amount != 40 {
		t.Fatalf("latest EXP: %+v %v", current, e)
	}
	if _, e = db.Exec("UPDATE pulse_reward_budget SET settled_amount=hard_cap WHERE period_id=? AND budget_type='community_exp'", newest.PeriodID); e != nil {
		t.Fatal(e)
	}
	refused := command("-budget", current.Selection)
	if _, e = action.Execute(ctx, refused); !errors.Is(e, ErrBudgetExceeded) {
		t.Fatalf("new budget limit: %v", e)
	}
	var receipts int
	db.QueryRow("SELECT COUNT(*) FROM pulse_idempotency WHERE scope IN (?,?) AND idempotency_key=? AND resource_type='action_refusal' AND response_status=409", fmt.Sprintf("pulse_action_identity:%d", user), fmt.Sprintf("pulse_action_request:%d", user), refused.ActionID).Scan(&receipts)
	if receipts != 2 {
		t.Fatalf("refusals not committed: %d", receipts)
	}
	if _, e = db.Exec("UPDATE pulse_reward_budget SET settled_amount=0 WHERE period_id=? AND budget_type='community_exp'", newest.PeriodID); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		retry := refused
		retry.IdempotencyKey = fmt.Sprintf("%s-budget-alias-%d", key, i)
		if _, e = action.Execute(ctx, retry); !errors.Is(e, ErrBudgetExceeded) {
			t.Fatalf("refusal replay: %v", e)
		}
	}
	rollbackErr := errors.New("simulated commit failure")
	broken, _ := NewActionService(rollbackSelectionUnit{unit, rollbackErr}, ActionConfig{RandomSecret: secret, DisableNewActions: true})
	rollbackCmd := command("-rollback", current.Selection)
	if _, e = broken.Execute(ctx, rollbackCmd); !errors.Is(e, rollbackErr) {
		t.Fatal(e)
	}
	db.QueryRow("SELECT COUNT(*) FROM pulse_idempotency WHERE scope=? AND idempotency_key=?", fmt.Sprintf("pulse_action_identity:%d", user), rollbackCmd.ActionID).Scan(&receipts)
	if receipts != 0 {
		t.Fatal("refusal survived rollback")
	}
	if _, e = action.Execute(ctx, rollbackCmd); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	failures := make([]error, 2)
	for i := range failures {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, failures[i] = action.Execute(ctx, command(fmt.Sprintf("-race-%d", i), current.Selection))
		}(i)
	}
	wg.Wait()
	successes := 0
	for _, err := range failures {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrSelectionChanged) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("last ticket race: %v", failures)
	}
	var grants, spends, allocations int
	db.QueryRow("SELECT COUNT(*) FROM pulse_reward_grant WHERE user_id=?", user).Scan(&grants)
	db.QueryRow("SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND operation='ticket_spend'", user).Scan(&spends)
	db.QueryRow("SELECT COUNT(*) FROM pulse_ticket_allocation a JOIN pulse_ticket_lot l ON l.id=a.lot_id WHERE l.user_id=?", user).Scan(&allocations)
	if grants != 7 || spends != grants || allocations != grants {
		t.Fatalf("facts diverged %d/%d/%d", grants, spends, allocations)
	}
	// The old ticket account is still debited under its origin, so rebuilding
	// accounts from immutable ledger entries remains exact after cross-rule draws.
	if e = unit.Do(ctx, func(r ports.Repositories) error {
		accounts, err := r.Account.ListForUser(ctx, user)
		if err != nil {
			return err
		}
		for _, a := range accounts {
			entries, err := r.Ledger.ListAccountEntries(ctx, user, a.PeriodID, a.AssetType)
			if err != nil {
				return err
			}
			rebuilt, err := ledger.Rebuild(ledger.Account{UserID: user, PeriodID: a.PeriodID, AssetType: a.AssetType}, entries)
			if err != nil {
				return err
			}
			if rebuilt.Balance != a.Balance {
				t.Fatal("account/ledger mismatch")
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}

type rollbackSelectionUnit struct {
	ports.UnitOfWork
	failure error
}

func (u rollbackSelectionUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return u.UnitOfWork.Do(ctx, func(r ports.Repositories) error {
		if err := fn(r); err != nil {
			return err
		}
		return u.failure
	})
}

// Gate only the rule mutex; business reads and writes remain real MySQL calls.
type gatedRuleUnit struct {
	ports.UnitOfWork
	ready, resume chan struct{}
}

func (u gatedRuleUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return u.UnitOfWork.Do(ctx, func(r ports.Repositories) error {
		r.Idempotency = gatedRuleIdempotency{r.Idempotency, u.ready, u.resume}
		return fn(r)
	})
}

type gatedRuleIdempotency struct {
	ports.IdempotencyRepository
	ready, resume chan struct{}
}

func (r gatedRuleIdempotency) GetOrCreateForUpdate(ctx context.Context, scope, key, hash string) (ports.IdempotencyRecord, error) {
	if scope == "period_create_lock" {
		close(r.ready)
		select {
		case <-r.resume:
		case <-ctx.Done():
			return ports.IdempotencyRecord{}, ctx.Err()
		}
	}
	return r.IdempotencyRepository.GetOrCreateForUpdate(ctx, scope, key, hash)
}
