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

func TestMySQLTicketGroupsRefusalsAndStaleSelection(t *testing.T) {
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
	catalog, e := rules.GetForUser(ctx, user)
	if e != nil || len(catalog.Groups) != 3 || catalog.Enabled {
		t.Fatalf("groups: %+v %v", catalog, e)
	}
	find := func(id uint64, exp bool) TicketRuleGroup {
		t.Helper()
		catalog, e := rules.GetForUser(ctx, user)
		if e != nil {
			t.Fatal(e)
		}
		for _, g := range catalog.Groups {
			if g.Period.ID == id && g.ExperienceOnly == exp {
				return g
			}
		}
		t.Fatalf("group missing: %d/%t", id, exp)
		return TicketRuleGroup{}
	}
	oldMixed, oldExp, newMixed := find(old.PeriodID, false), find(old.PeriodID, true), find(fresh.PeriodID, false)
	if oldMixed.Enabled || oldExp.Enabled || !newMixed.Enabled || oldMixed.TotalWeight != 2 || oldExp.TotalWeight != 1 || oldMixed.TicketCount != 2 || oldExp.TicketCount != 2 || newMixed.TicketCount != 3 || newMixed.NextLotRemaining != 2 {
		t.Fatal("incorrect group/odds/capacity")
	}
	if oldMixed.Budgets[1].ID != oldExp.Budgets[0].ID {
		t.Fatal("EXP budget duplicated by eligibility view")
	}
	action, _ := NewActionService(unit, ActionConfig{RandomSecret: secret, RequireVerifiedFunding: true, Now: func() time.Time { return now }})
	command := func(id, selection string) ActionCommand {
		return ActionCommand{ProtocolVersion: 2, Selection: selection, UserID: user, ActionID: key + id, IdempotencyKey: key + id, TriggerType: ActionTriggerType}
	}
	refused := command("-refusal", oldMixed.Selection)
	if _, e = action.Execute(ctx, refused); !errors.Is(e, ErrBudgetExceeded) {
		t.Fatalf("old budget: %v", e)
	}
	var receipts int
	if e = db.QueryRow("SELECT COUNT(*) FROM pulse_idempotency WHERE scope IN (?,?) AND resource_type='action_refusal' AND response_status=409", fmt.Sprintf("pulse_action_identity:%d", user), fmt.Sprintf("pulse_action_request:%d", user)).Scan(&receipts); e != nil || receipts != 2 {
		t.Fatalf("refusal not committed: %d %v", receipts, e)
	}
	// A new group is usable while the old one is still paused.
	newCmd := command("-new-draw", newMixed.Selection)
	first, e := action.Execute(ctx, newCmd)
	if e != nil || first.PeriodID != fresh.PeriodID {
		t.Fatalf("old group blocked new: %+v %v", first, e)
	}
	changed := newCmd
	changed.Selection = oldExp.Selection
	if _, e = action.Execute(ctx, changed); !errors.Is(e, ledger.ErrIdempotencyConflict) {
		t.Fatalf("changed selection: %v", e)
	}
	changed = newCmd
	changed.ProtocolVersion = 0
	changed.Selection = ""
	if _, e = action.Execute(ctx, changed); !errors.Is(e, ledger.ErrIdempotencyConflict) {
		t.Fatalf("downgrade fingerprint: %v", e)
	}
	// Fixtures may replenish counters; immutable limits and rules stay untouched.
	if _, e = db.Exec("UPDATE pulse_reward_budget SET settled_amount=0 WHERE period_id=? AND budget_type='community_exp'", old.PeriodID); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		retry := refused
		if i > 0 {
			retry.IdempotencyKey = fmt.Sprintf("%s-alias-%d", key, i)
		}
		if _, e = action.Execute(ctx, retry); !errors.Is(e, ErrBudgetExceeded) {
			t.Fatalf("refusal changed after recovery: %v", e)
		}
	}
	if _, e = action.Execute(ctx, command("-reconfirmed", oldMixed.Selection)); e != nil {
		t.Fatal(e)
	}
	// Two actions race for the final ticket of the selected batch. A later batch
	// in the SAME group must not be silently selected by the losing request.
	var wg sync.WaitGroup
	failures := make([]error, 2)
	for i := range failures {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, failures[i] = action.Execute(ctx, command(fmt.Sprintf("-race-%d", i), newMixed.Selection))
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
		t.Fatalf("batch race: %v", failures)
	}
	after := find(fresh.PeriodID, false)
	if after.TicketCount != 1 || after.Selection == newMixed.Selection {
		t.Fatal("later batch not exposed as a new choice")
	}
	// Lock waits can cross a deadline after the initial validation.
	clockCalls := 0
	boundaryAction, _ := NewActionService(unit, ActionConfig{RandomSecret: secret, Now: func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return now
		}
		return *after.QuotaExpiresAt
	}})
	if _, e = boundaryAction.Execute(ctx, command("-wait-crossed-expiry", after.Selection)); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("lock wait crossed expiry: %v", e)
	}
	// Authentication and integrity must be checked even for a structurally valid token.
	foreign := command("-foreign", after.Selection)
	foreign.UserID++
	if _, e = action.Execute(ctx, foreign); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("foreign selection: %v", e)
	}
	tampered := command("-tampered", after.Selection+"a")
	if _, e = action.Execute(ctx, tampered); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("tampered selection: %v", e)
	}
	legacy := command("-legacy-unsubmitted", "")
	legacy.ProtocolVersion = 0
	if _, e = action.Execute(ctx, legacy); !errors.Is(e, ErrSelectionRequired) {
		t.Fatalf("legacy guessed a choice: %v", e)
	}
	// A mixed preview cannot turn into an EXP request, even at the exact deadline.
	now = *after.QuotaExpiresAt
	expiry := command("-expiry", after.Selection)
	if _, e = action.Execute(ctx, expiry); !errors.Is(e, ErrSelectionChanged) {
		t.Fatalf("expiry silently switched mode: %v", e)
	}
	action.secret = []byte("rotated-root")
	action.cfg.DisableNewActions = true
	for i := 0; i < 100; i++ {
		got, err := action.Execute(ctx, newCmd)
		if err != nil || got != first {
			t.Fatalf("response loss recovery: %+v %v", got, err)
		}
	}
	// A transaction rollback must not produce a persistent no-charge receipt.
	action.secret = secret
	action.cfg.DisableNewActions = false
	expired := find(fresh.PeriodID, true)
	rollbackErr := errors.New("simulated commit failure")
	broken, _ := NewActionService(rollbackSelectionUnit{unit, rollbackErr}, ActionConfig{RandomSecret: secret, DisableNewActions: true})
	rollbackCmd := command("-rollback", expired.Selection)
	if _, e = broken.Execute(ctx, rollbackCmd); !errors.Is(e, rollbackErr) {
		t.Fatal(e)
	}
	if e = db.QueryRow("SELECT COUNT(*) FROM pulse_idempotency WHERE scope=? AND idempotency_key=?", fmt.Sprintf("pulse_action_identity:%d", user), rollbackCmd.ActionID).Scan(&receipts); e != nil || receipts != 0 {
		t.Fatalf("rolled back refusal remained: %d %v", receipts, e)
	}
	if _, e = action.Execute(ctx, rollbackCmd); e != nil {
		t.Fatalf("rollback was cached as refusal: %v", e)
	}
	// Cross-check ledger, allocations and immutable grants after all failures.
	var grants, spends, allocations int
	db.QueryRow("SELECT COUNT(*) FROM pulse_reward_grant WHERE user_id=?", user).Scan(&grants)
	db.QueryRow("SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND operation='ticket_spend'", user).Scan(&spends)
	db.QueryRow("SELECT COUNT(*) FROM pulse_ticket_allocation a JOIN pulse_ticket_lot l ON l.id=a.lot_id WHERE l.user_id=?", user).Scan(&allocations)
	if grants != 4 || spends != grants || allocations != grants {
		t.Fatalf("facts diverged: %d/%d/%d", grants, spends, allocations)
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
