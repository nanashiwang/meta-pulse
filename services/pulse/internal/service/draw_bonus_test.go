package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func drawBonusDefinitions(t *testing.T) []reward.Definition {
	t.Helper()
	specs, err := CheckinRewardPrizes(100)
	if err != nil {
		t.Fatal(err)
	}
	var defs []reward.Definition
	for _, spec := range specs {
		defs = append(defs, reward.Definition{ID: uint64(len(defs) + 1), RewardKey: spec.Key, RewardType: spec.RewardType, Amount: spec.Amount, Weight: spec.Weight, ConfigVersion: DrawBonusPolicyKey, Enabled: true})
	}
	return defs
}

func TestBonusDrawKeepsEveryMainPrizeQuotaAndExpiredPrizesExperienceOnly(t *testing.T) {
	defs := drawBonusDefinitions(t)
	var bonusCount int
	for i := 0; i < 10000; i++ {
		seed, err := reward.Derive([]byte("draw-bonus-policy-test"), 4, 9, fmt.Sprintf("draw-%d", i), DrawBonusPolicyKey)
		if err != nil {
			t.Fatal(err)
		}
		p, exp, err := selectDrawPrizes(defs, seed, true, false)
		if err != nil || p.RewardType != "newapi_quota" {
			t.Fatalf("main = %+v, %v", p, err)
		}
		if exp > 0 {
			bonusCount++
			if exp != 1 && exp != 4 && exp != 20 {
				t.Fatal("unexpected bonus", exp)
			}
		}
		p2, e2, err := selectDrawPrizes(defs, seed, true, false)
		if p2 != p || e2 != exp || err != nil {
			t.Fatal("not replayable")
		}
		p, exp, err = selectDrawPrizes(defs, seed, true, true)
		if err != nil || p.RewardType != ExperienceRewardType || exp != 0 {
			t.Fatalf("expired = %+v, %d, %v", p, exp, err)
		}
	}
	if bonusCount < 900 || bonusCount > 1100 {
		t.Fatal("bonus frequency", bonusCount)
	}
}

func TestDrawBonusReservesTwoBudgetsButKeepsOneGrantAndOneSpend(t *testing.T) {
	for _, exhaust := range []bool{false, true} {
		store, rewards, idem := setupActionStore()
		store.periods[0].ConfigVersion = DrawBonusPolicyKey
		rewards.definitions = drawBonusDefinitions(t)
		rewards.budgets[budgetKey(4, ActionBudgetType)] = ports.RewardBudget{ID: 3, PeriodID: 4, BudgetType: ActionBudgetType, HardCap: 10000}
		budget := ports.RewardBudget{ID: 5, PeriodID: 4, BudgetType: ExperienceRewardType, HardCap: 20}
		if exhaust {
			budget.ReservedAmount = 1
		}
		rewards.budgets[budgetKey(4, ExperienceRewardType)] = budget
		svc := newActionService(t, store, rewards, idem)
		svc.cfg.ShadowMode = false
		var action string
		for i := 0; i < 100; i++ {
			candidate := fmt.Sprintf("has-bonus-%d", i)
			seed, _ := reward.Derive(svc.secret, 4, 9, candidate, DrawBonusPolicyKey)
			_, b, err := selectDrawPrizes(rewards.definitions, seed, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if b > 0 {
				action = candidate
				break
			}
		}
		if action == "" {
			t.Fatal("fixture lacks a bonus")
		}
		command := ActionCommand{ProtocolVersion: 3, Selection: signSelection(svc.secret, 9, store.periods[0], nil, false), UserID: 9, ActionID: action, TriggerType: ActionTriggerType, IdempotencyKey: "draw"}
		got, err := svc.Execute(context.Background(), command)
		if exhaust {
			if !errors.Is(err, ErrBudgetExceeded) || len(rewards.grants) != 0 || len(store.entries) != 0 {
				t.Fatalf("exhausted: %+v %v", got, err)
			}
			continue
		}
		if err != nil || got.BonusExperience <= 0 || got.BonusExperienceStatus != "pending" {
			t.Fatalf("first draw %+v %v", got, err)
		}
		for i := 0; i < 100; i++ {
			replay, err := svc.Execute(context.Background(), command)
			if err != nil || replay != got {
				t.Fatalf("replay %+v %v", replay, err)
			}
		}
		if len(rewards.grants) != 1 || len(rewards.outboxes) != 1 || len(store.entries) != 1 || store.accounts[accountKey(9, 4, ledger.AssetTicket)].Balance != 0 || rewards.budgets[budgetKey(4, ExperienceRewardType)].ReservedAmount != got.BonusExperience || rewards.budgets[budgetKey(4, ActionBudgetType)].ReservedAmount != got.Amount {
			t.Fatal("grant, ticket or reservation duplicated")
		}
	}
}

func TestBonusPreviewUsesQuotaDenominatorAndPreservesExpiry(t *testing.T) {
	store, rewards, idem := setupActionStore()
	p := store.periods[0]
	p.ConfigVersion = DrawBonusPolicyKey
	p.FundingPolicy = period.VerifiedPaidFunding
	p.TicketThresholdMilli = 1
	rewards.definitions = drawBonusDefinitions(t)
	rewards.budgets[budgetKey(4, ActionBudgetType)] = ports.RewardBudget{ID: 3, PeriodID: 4, BudgetType: ActionBudgetType, HardCap: 10000}
	rewards.budgets[budgetKey(4, ExperienceRewardType)] = ports.RewardBudget{ID: 5, PeriodID: 4, BudgetType: ExperienceRewardType, HardCap: 20}
	svc := NewRewardRulesService(actionUnit{store: store, reward: rewards, idem: idem}, true)
	at := p.StartsAt.Add(time.Minute)
	err := svc.unit.Do(context.Background(), func(r ports.Repositories) error {
		rules, budgets, err := svc.ruleView(context.Background(), r, p, nil, at)
		if err != nil || !rules.Enabled || len(budgets) != 2 || len(rules.Rewards) != 6 || rules.TotalWeight != 900 || rules.BonusExperiencePercent != 10 || rules.ExperienceTotalWeight != 100 {
			t.Fatalf("eligible rules %+v budgets %v err %v", rules, budgets, err)
		}
		expired, budgets, err := svc.ruleView(context.Background(), r, p, &ports.TicketLot{QuotaExpiresAt: at}, at)
		if err != nil || !expired.Enabled || !expired.ExperienceOnly || len(expired.Rewards) != 3 || len(budgets) != 1 || expired.TotalWeight != 100 || expired.BonusExperiencePercent != 0 {
			t.Fatalf("expired %+v budgets %v err %v", expired, budgets, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type testDrawBonusDelivery struct {
	ports.ExperienceDelivery
	g *ports.RewardGrant
}

func (d testDrawBonusDelivery) TransitionDrawBonus(_ context.Context, id uint64, from, to string) error {
	if d.g.ID != id || d.g.BonusExperienceStatus != from {
		return ports.ErrConflict
	}
	d.g.BonusExperienceStatus = to
	return nil
}

func TestDrawBonusAcknowledgementAndReversalDoNotTouchFlash(t *testing.T) {
	ctx := context.Background()
	store := newMemoryRewardStore()
	store.budgets[budgetKey(4, ExperienceRewardType)] = ports.RewardBudget{ID: 5, PeriodID: 4, BudgetType: ExperienceRewardType, HardCap: 100, ReservedAmount: 4}
	g := ports.RewardGrant{ID: 3, PeriodID: 4, RewardType: "newapi_quota", Amount: 100, Status: "pending", BonusExperience: 4, BonusExperienceStatus: "pending"}
	repos := ports.Repositories{Reward: store, Experience: testDrawBonusDelivery{g: &g}}
	for i := 0; i < 100; i++ {
		if err := acknowledgeDrawBonus(ctx, repos, g, "pending"); err != nil {
			t.Fatal(err)
		}
	}
	b := store.budgets[budgetKey(4, ExperienceRewardType)]
	if g.BonusExperienceStatus != "settled" || g.Status != "pending" || b.ReservedAmount != 0 || b.SettledAmount != 4 {
		t.Fatalf("settled %+v %+v", g, b)
	}
	for i := 0; i < 100; i++ {
		if err := reverseDrawBonus(ctx, repos, g); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 100; i++ {
		if err := acknowledgeDrawBonus(ctx, repos, g, "reversed"); err != nil {
			t.Fatal(err)
		}
	}
	b = store.budgets[budgetKey(4, ExperienceRewardType)]
	if g.BonusExperienceStatus != "reversed" || g.Status != "pending" || b.SettledAmount != 0 || b.ReleasedAmount != 4 {
		t.Fatalf("reversed %+v %+v", g, b)
	}
	if err := acknowledgeDrawBonus(ctx, repos, g, "pending"); !errors.Is(err, ports.ErrConflict) {
		t.Fatal("stale acknowledgement", err)
	}
}
