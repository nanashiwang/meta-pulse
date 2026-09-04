package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestActionReplaySurvivesClosedAndChangedPeriods(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			store, rewards, idem := setupActionStore()
			action := newActionService(t, store, rewards, idem)
			cmd := ActionCommand{UserID: 9, ActionID: "lost-response", TriggerType: ActionTriggerType, IdempotencyKey: "request"}
			first, err := action.Execute(context.Background(), cmd)
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				// Emulate the old binary's durable request, not just a new scope alias.
				idem.records = make(map[string]ports.IdempotencyRecord)
				record, err := idem.GetOrCreateForUpdate(context.Background(), "pulse_action:4:9", cmd.IdempotencyKey, actionPayloadHash(cmd, first.PeriodID, first.ConfigVersion))
				if err != nil {
					t.Fatal(err)
				}
				if err := saveActionIdempotency(context.Background(), idem, record, first); err != nil {
					t.Fatal(err)
				}
			}
			store.periods[0].Status = period.StatusClosed
			// Replay must not re-randomize even if runtime configuration rotated.
			action.secret = []byte("rotated-secret")
			for i := 0; i < 100; i++ {
				got, err := action.Execute(context.Background(), cmd)
				if err != nil || got != first {
					t.Fatalf("closed replay %d: got=%+v err=%v", i, got, err)
				}
			}
			at := time.Unix(1_700_000_000, 0).UTC()
			store.periods = append(store.periods, period.Period{ID: 5, Key: "p5", Status: period.StatusActive, StartsAt: at.Add(-time.Minute), EndsAt: at.Add(time.Hour), ConfigVersion: "v2"})
			store.accounts[accountKey(9, 5, ledger.AssetTicket)] = ledger.Account{ID: 2, UserID: 9, PeriodID: 5, AssetType: ledger.AssetTicket, Balance: 1}
			rewards.budgets[budgetKey(5, ActionBudgetType)] = ports.RewardBudget{ID: 4, PeriodID: 5, BudgetType: ActionBudgetType, HardCap: 100}
			for i := 0; i < 100; i++ {
				// Aliases for an existing action must still return its original response.
				if i > 0 {
					cmd.IdempotencyKey = fmt.Sprintf("alias-%d", i)
				}
				got, err := action.Execute(context.Background(), cmd)
				if err != nil || got != first {
					t.Fatalf("next-period replay %d: got=%+v err=%v", i, got, err)
				}
			}
			cmd.IdempotencyKey = "request"
			cmd.ActionID = "different-payload"
			if _, err := action.Execute(context.Background(), cmd); !errors.Is(err, ledger.ErrIdempotencyConflict) {
				t.Fatalf("changed payload err=%v", err)
			}
			if len(rewards.grants) != 1 || len(rewards.outboxes) != 1 || len(store.entries) != 1 || store.accounts[accountKey(9, 5, ledger.AssetTicket)].Balance != 1 || rewards.budgets[budgetKey(5, ActionBudgetType)].ReservedAmount != 0 {
				t.Fatal("replay mutated financial facts")
			}
		})
	}
}

func TestActionLegacyRequestsFailClosed(t *testing.T) {
	for _, scenario := range []string{"changed_payload", "ambiguous_key", "ambiguous_action", "incomplete"} {
		t.Run(scenario, func(t *testing.T) {
			store, rewards, idem := setupActionStore()
			action := newActionService(t, store, rewards, idem)
			cmd := ActionCommand{UserID: 9, ActionID: "old-action", TriggerType: ActionTriggerType, IdempotencyKey: "old-key"}
			first, err := action.Execute(context.Background(), cmd)
			if err != nil {
				t.Fatal(err)
			}
			idem.records = make(map[string]ports.IdempotencyRecord)
			old, _ := idem.GetOrCreateForUpdate(context.Background(), "pulse_action:4:9", cmd.IdempotencyKey, actionPayloadHash(cmd, first.PeriodID, first.ConfigVersion))
			if scenario != "incomplete" {
				if err := saveActionIdempotency(context.Background(), idem, old, first); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "changed_payload":
				cmd.ActionID = "new-action"
			case "ambiguous_key":
				extra := old
				extra.Scope = "pulse_action:5:9"
				if err := idem.Save(context.Background(), extra); err != nil {
					t.Fatal(err)
				}
			case "ambiguous_action":
				duplicate := rewards.grants[0]
				duplicate.PeriodID = 5
				duplicate.GrantID = "duplicate"
				rewards.grants = append(rewards.grants, duplicate)
			}
			store.periods[0].Status = period.StatusClosed
			if _, err := action.Execute(context.Background(), cmd); !errors.Is(err, ledger.ErrIdempotencyConflict) {
				t.Fatalf("err=%v, want conflict", err)
			}
			if len(store.entries) != 1 {
				t.Fatal("conflict wrote another spend")
			}
		})
	}
}

func TestActionPreservesLegacyResponseAfterAliasRecovery(t *testing.T) {
	store, rewards, idem := setupActionStore()
	action := newActionService(t, store, rewards, idem)
	command := ActionCommand{UserID: 9, ActionID: "legacy-action", TriggerType: ActionTriggerType, IdempotencyKey: "original-key"}
	first, err := action.Execute(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	idem.records = make(map[string]ports.IdempotencyRecord)
	old, _ := idem.GetOrCreateForUpdate(context.Background(), "pulse_action:4:9", command.IdempotencyKey, actionPayloadHash(command, first.PeriodID, first.ConfigVersion))
	if err := saveActionIdempotency(context.Background(), idem, old, first); err != nil {
		t.Fatal(err)
	}
	rewards.grants[0].Status = GrantStatusSettled
	store.periods[0].Status = period.StatusClosed
	alias := command
	alias.IdempotencyKey = "new-alias"
	aliased, err := action.Execute(context.Background(), alias)
	if err != nil || aliased.GrantID != first.GrantID || aliased.Status != GrantStatusSettled {
		t.Fatalf("alias=%+v err=%v", aliased, err)
	}
	for i := 0; i < 100; i++ {
		got, err := action.Execute(context.Background(), command)
		if err != nil || got != first {
			t.Fatalf("original response changed: got=%+v first=%+v err=%v", got, first, err)
		}
	}
	if len(rewards.grants) != 1 || len(store.entries) != 1 {
		t.Fatal("alias recovery changed financial facts")
	}
}
