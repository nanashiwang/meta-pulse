package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type rewardAdminMemory struct {
	definitions    []reward.Definition
	budgets        []ports.RewardBudget
	failDefinition bool
	failBudget     bool
}

func (m *rewardAdminMemory) CreateDefinition(_ context.Context, _ uint64, d reward.Definition) (reward.Definition, error) {
	if m.failDefinition {
		return reward.Definition{}, errors.New("definition unavailable")
	}
	d.ID = uint64(len(m.definitions) + 1)
	m.definitions = append(m.definitions, d)
	return d, nil
}
func (m *rewardAdminMemory) CreateBudget(_ context.Context, b ports.RewardBudget) (ports.RewardBudget, error) {
	if m.failBudget {
		return ports.RewardBudget{}, errors.New("budget unavailable")
	}
	b.ID = uint64(len(m.budgets) + 1)
	m.budgets = append(m.budgets, b)
	return b, nil
}

func fundedPeriodCommand() PeriodCreateCommand {
	c := validCreateCommand()
	c.Rewards = []PeriodRewardSpec{{Key: "small", Amount: 5, Weight: 90}, {Key: "large", Amount: 20, Weight: 10}}
	c.RewardBudget = 100
	c.TicketThresholdMilli = 1000
	c.Activate = true
	return c
}

func TestCreateRewardPeriodFreezesPrizeBudgetFundingAndAudit(t *testing.T) {
	u := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(u, time.Now)
	r, err := svc.Create(context.Background(), fundedPeriodCommand())
	if err != nil {
		t.Fatal(err)
	}
	if r.FundingPolicy != period.VerifiedPaidFunding || r.TicketThresholdMilli != 1000 || r.Status != "active" || r.RewardCount != 2 || r.RewardBudget != 100 {
		t.Fatalf("result=%+v", r)
	}
	if len(u.reward.definitions) != 2 || len(u.reward.budgets) != 1 || u.reward.budgets[0].BudgetType != "loyalty" {
		t.Fatalf("pool=%+v", u.reward)
	}
	for _, d := range u.reward.definitions {
		if d.TransferableQuota || d.RewardType != "newapi_quota" || !d.Enabled || d.ConfigVersion != "v1" {
			t.Fatalf("unsafe definition=%+v", d)
		}
	}
	var audit struct {
		FundingPolicy        string             `json:"funding_policy"`
		Rewards              []PeriodRewardSpec `json:"rewards"`
		RewardBudget         int64              `json:"reward_budget"`
		TicketThresholdMilli int64              `json:"ticket_threshold_milli"`
	}
	if err := json.Unmarshal(u.audit.logs[0].AfterJSON, &audit); err != nil {
		t.Fatal(err)
	}
	if audit.FundingPolicy != period.VerifiedPaidFunding || len(audit.Rewards) != 2 || audit.RewardBudget != 100 || audit.TicketThresholdMilli != 1000 {
		t.Fatalf("audit=%+v", audit)
	}
}

func TestCreateRewardPeriodRollsBackEveryFailure(t *testing.T) {
	for _, stage := range []string{"definition", "budget", "audit"} {
		t.Run(stage, func(t *testing.T) {
			u := newPeriodCreateUnit()
			switch stage {
			case "definition":
				u.reward.failDefinition = true
			case "budget":
				u.reward.failBudget = true
			case "audit":
				u.audit.err = errors.New("audit unavailable")
			}
			svc, _ := NewPeriodCreateService(u, time.Now)
			if _, err := svc.Create(context.Background(), fundedPeriodCommand()); err == nil {
				t.Fatal("failure accepted")
			}
			if len(u.admin.periods)+len(u.economics.rules)+len(u.reward.definitions)+len(u.reward.budgets)+len(u.audit.logs) != 0 {
				t.Fatal("partial reward setup survived rollback")
			}
		})
	}
}

func TestCreateRewardPeriodRejectsUnsafeEconomics(t *testing.T) {
	for name, mutate := range map[string]func(*PeriodCreateCommand){
		"missing budget":         func(c *PeriodCreateCommand) { c.RewardBudget = 0 },
		"missing threshold":      func(c *PeriodCreateCommand) { c.TicketThresholdMilli = 0 },
		"empty pool with budget": func(c *PeriodCreateCommand) { c.Rewards = nil },
		"duplicate keys":         func(c *PeriodCreateCommand) { c.Rewards[1].Key = c.Rewards[0].Key },
		"noncanonical key":       func(c *PeriodCreateCommand) { c.Rewards[0].Key = " Small " },
		"negative amount":        func(c *PeriodCreateCommand) { c.Rewards[0].Amount = -1 },
		"zero prize":             func(c *PeriodCreateCommand) { c.Rewards[0].Amount = 0 },
		"prize exceeds budget":   func(c *PeriodCreateCommand) { c.Rewards[0].Amount = 101 },
		"zero weight":            func(c *PeriodCreateCommand) { c.Rewards[0].Weight = 0 },
		"weight sum overflow":    func(c *PeriodCreateCommand) { c.Rewards[0].Weight = maxPublicRewardInteger },
		"unsafe amount":          func(c *PeriodCreateCommand) { c.RewardBudget = 1 << 60; c.Rewards[0].Amount = 1 << 53 },
	} {
		t.Run(name, func(t *testing.T) {
			u := newPeriodCreateUnit()
			svc, _ := NewPeriodCreateService(u, time.Now)
			c := fundedPeriodCommand()
			mutate(&c)
			if _, err := svc.Create(context.Background(), c); err == nil {
				t.Fatal("unsafe pool accepted")
			}
			if len(u.admin.periods) != 0 {
				t.Fatal("invalid pool began writing")
			}
		})
	}
}

func TestCreateLegacyPeriodRemainsIneligibleForCashRewards(t *testing.T) {
	u := newPeriodCreateUnit()
	svc, _ := NewPeriodCreateService(u, time.Now)
	r, err := svc.Create(context.Background(), validCreateCommand())
	if err != nil {
		t.Fatal(err)
	}
	if r.FundingPolicy != "legacy" || r.TicketThresholdMilli != 0 || len(u.reward.definitions) != 0 || len(u.reward.budgets) != 0 {
		t.Fatalf("legacy result=%+v", r)
	}
}
