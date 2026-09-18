package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestReversedActionReplayCannotSpendOrGrantAgain(t *testing.T) {
	ctx := context.Background()
	store, rewards, idem := setupActionStore()
	rewards.definitions[0].RewardType = "newapi_quota"
	action := newActionService(t, store, rewards, idem)
	action.cfg.ShadowMode = false
	command := ActionCommand{UserID: 9, ActionID: "one-action", TriggerType: "pulse", IdempotencyKey: "original-key"}
	first, err := action.Execute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeBenefitClient{
		grantResponse: ports.BenefitGrantResponse{Applied: true, SourceRef: first.GrantID},
		rollbackState: ports.BenefitState{RolledBack: true, Status: ports.BenefitStatusRolledBack, SourceRef: first.GrantID},
	}
	outboxes := &memorySettlementStore{outboxes: append([]ports.SettlementOutbox(nil), rewards.outboxes...)}
	settlement, err := NewSettlementService(settlementUnit{store: store, reward: rewards, settlement: outboxes}, client, SettlementConfig{
		BatchSize: 10, Lease: time.Minute, BaseBackoff: time.Second, MaxBackoff: time.Minute, MaxAttempts: 3, Now: action.cfg.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report, err := settlement.ProcessBatch(ctx); err != nil || report.Completed != 1 {
		t.Fatalf("settlement report=%+v err=%v", report, err)
	}
	grantID := rewards.grants[0].ID
	if err := settlement.Rollback(ctx, grantID, "operator correction"); err != nil {
		t.Fatal(err)
	}
	// A response lost before settlement may be retried after an operator has
	// already reversed the reward. Neither request-key aliases nor a paused
	// action endpoint may turn the historical action into a new transaction.
	action.cfg.DisableNewActions = true
	for i := 0; i < 100; i++ {
		if i%2 != 0 {
			command.IdempotencyKey = fmt.Sprintf("retry-alias-%d", i)
		} else {
			command.IdempotencyKey = "original-key"
		}
		got, err := action.Execute(ctx, command)
		if err != nil || got != first {
			t.Fatalf("replay %d got=%+v err=%v", i, got, err)
		}
		if err := settlement.Rollback(ctx, grantID, "operator retry"); err != nil {
			t.Fatal(err)
		}
	}
	if report, err := settlement.ProcessBatch(ctx); err != nil || report.Completed != 0 {
		t.Fatalf("completed outbox ran again: report=%+v err=%v", report, err)
	}
	budget := rewards.budgets[budgetKey(4, ActionBudgetType)]
	if budget.ReservedAmount != 0 || budget.SettledAmount != 0 || budget.ReleasedAmount != first.Amount {
		t.Fatalf("budget=%+v", budget)
	}
	if len(store.entries) != 1 || len(rewards.grants) != 1 || len(rewards.outboxes) != 1 || store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance != 0 {
		t.Fatalf("entries=%d grants=%d outboxes=%d tickets=%d", len(store.entries), len(rewards.grants), len(rewards.outboxes), store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance)
	}
	if rewards.grants[0].Status != GrantStatusReversed || client.grantCalls != 1 || client.rollbackCalls != 1 {
		t.Fatalf("grant=%+v grant_calls=%d rollback_calls=%d", rewards.grants[0], client.grantCalls, client.rollbackCalls)
	}
}

func TestUnconfirmedRollbackRetainsSettledBudgetUntilConfirmed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state ports.BenefitState
		err   error
	}{
		{name: "insufficient balance", err: ports.ErrBenefitPayloadConflict}, // Receiver HTTP 409 is surfaced as a conflict by the client.
		{name: "response lost", err: context.DeadlineExceeded},
		{name: "still applied", state: ports.BenefitState{Applied: true, SourceRef: "pg_test"}},
		{name: "wrong source", state: ports.BenefitState{RolledBack: true, SourceRef: "another-grant"}},
		{name: "ambiguous flags", state: ports.BenefitState{RolledBack: true, Applied: true, SourceRef: "pg_test"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := &fakeBenefitClient{grantResponse: ports.BenefitGrantResponse{Applied: true, SourceRef: "pg_test"}}
			settlement, rewards, outboxes, _ := settlementFixture(t, client)
			if report, err := settlement.ProcessBatch(ctx); err != nil || report.Completed != 1 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
			before := rewards.budgets[budgetKey(4, ActionBudgetType)]
			client.rollbackState, client.rollbackErr = tc.state, tc.err
			if err := settlement.Rollback(ctx, 1, "operator correction"); err == nil {
				t.Fatal("unconfirmed rollback succeeded")
			} else if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("err=%v want=%v", err, tc.err)
			}
			if after := rewards.budgets[budgetKey(4, ActionBudgetType)]; after != before || rewards.grants[0].Status != GrantStatusSettled || outboxes.outboxes[0].Status != OutboxStatusCompleted {
				t.Fatalf("unconfirmed rollback changed local financial state: budget=%+v grant=%+v", after, rewards.grants[0])
			}
			client.rollbackErr = nil
			client.rollbackState = ports.BenefitState{RolledBack: true, Status: ports.BenefitStatusRolledBack, SourceRef: "pg_test"}
			for i := 0; i < 100; i++ {
				if err := settlement.Rollback(ctx, 1, "operator retry"); err != nil {
					t.Fatal(err)
				}
			}
			budget := rewards.budgets[budgetKey(4, ActionBudgetType)]
			if budget.ReservedAmount != 0 || budget.SettledAmount != 0 || budget.ReleasedAmount != 10 || client.grantCalls != 1 || client.rollbackCalls != 2 || client.rollbackRef != "pg_test" {
				t.Fatalf("budget=%+v client=%+v", budget, client)
			}
		})
	}
}
