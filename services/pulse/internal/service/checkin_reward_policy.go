package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const CheckinRewardPolicyKey = "rules-checkin-20261117-v1"

// CheckinRewardPrizes has exactly 2.5 units of quota expectation per ticket,
// including the 10% experience-only outcomes in the denominator.
func CheckinRewardPrizes(unit int64) ([]PeriodRewardSpec, error) {
	if unit <= 0 || unit > int64(maxPublicRewardInteger)/100 {
		return nil, ErrInvalidPeriod
	}
	tiers := [][2]int64{{1, 450}, {2, 300}, {5, 100}, {10, 40}, {50, 9}, {100, 1}}
	rows := make([]PeriodRewardSpec, 0, 9)
	for _, t := range tiers {
		rows = append(rows, PeriodRewardSpec{Key: fmt.Sprintf("api-%d", t[0]), RewardType: "newapi_quota", Amount: unit * t[0], Weight: uint64(t[1])})
	}
	for _, t := range [][2]int64{{1, 50}, {4, 30}, {20, 20}} {
		rows = append(rows, PeriodRewardSpec{Key: fmt.Sprintf("exp-%d", t[0]), RewardType: ExperienceRewardType, Amount: t[0], Weight: uint64(t[1])})
	}
	return rows, nil
}

// ApplyCheckinRewardPolicy runs only during the explicit deployment upgrade.
// A permanent rule key makes future upgrades preserve later administrator rules.
// Create still owns CAS, immutable economics, idempotency, locks and audit.
func (s *PeriodCreateService) ApplyCheckinRewardPolicy(ctx context.Context, unit int64) (PeriodCreateResult, error) {
	var cmd PeriodCreateCommand
	var existing PeriodCreateResult
	err := s.unit.Do(ctx, func(r ports.Repositories) error {
		if err := lockRewardRule(ctx, r.Idempotency); err != nil {
			return err
		}
		p, err := r.PeriodAdmin.FindByKeyForUpdate(ctx, CheckinRewardPolicyKey)
		if err == nil {
			if p.Status == period.StatusDraft {
				return errors.New("check-in policy key belongs to an unpublished draft")
			}
			existing = PeriodCreateResult{PeriodID: p.ID, Key: p.Key, Status: string(p.Status)}
			return nil
		}
		if !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		p, err = r.Period.FindActiveAtCurrent(ctx, s.now())
		if errors.Is(err, period.ErrNoActivePeriod) {
			return nil
		}
		if err != nil {
			return err
		}
		if !p.Continuous {
			return errors.New("check-in policy requires current continuous rules")
		}
		prizes, err := CheckinRewardPrizes(unit)
		if err != nil {
			return err
		}
		rules, err := r.Economics.ListRules(ctx, p.ID)
		if err != nil {
			return err
		}
		cmd = PeriodCreateCommand{Continuous: true, QuotaValidityDays: p.QuotaValidityDays, ExpectedPeriodID: p.ID, RequestID: CheckinRewardPolicyKey, ActorType: "deployment", ActorID: "checkin-reward-policy", Key: CheckinRewardPolicyKey, ConfigVersion: CheckinRewardPolicyKey, RandomVersion: CheckinRewardPolicyKey, Timezone: p.Timezone, Activate: true, TicketThresholdMilli: p.TicketThresholdMilli, Rewards: prizes, Reason: "周一周五签到各赠3券，发布每券期望2.5闪的奖池"}
		for _, rule := range rules {
			cmd.Rules = append(cmd.Rules, PeriodRuleSpec{Key: rule.Key, Priority: rule.Priority, ModelPattern: rule.ModelPattern, ChannelID: rule.ChannelID, Eligible: rule.Eligible, MultiplierBps: int32(rule.MultiplierBps)})
		}
		for _, kind := range []string{ActionBudgetType, ExperienceRewardType} {
			budget, err := r.Reward.GetBudget(ctx, p.ID, kind)
			if err != nil {
				return err
			}
			if kind == ActionBudgetType {
				cmd.QuotaBudgetUnlimited = budget.Unlimited
				cmd.RewardBudget = budget.HardCap
			} else {
				cmd.ExperienceBudgetUnlimited = budget.Unlimited
				cmd.ExperienceBudget = budget.HardCap
			}
		}
		return nil
	})
	if err != nil || existing.PeriodID != 0 || cmd.Key == "" {
		return existing, err
	}
	result, err := s.Create(ctx, cmd)
	if err != nil {
		// Another deployment may have won after the read transaction. Only the
		// same permanent policy key proves completion; unrelated CAS conflicts fail.
		var winner period.Period
		queryErr := s.unit.Do(ctx, func(r ports.Repositories) error {
			var e error
			winner, e = r.PeriodAdmin.FindByKeyForUpdate(ctx, CheckinRewardPolicyKey)
			return e
		})
		if queryErr == nil && winner.Status != period.StatusDraft {
			return PeriodCreateResult{PeriodID: winner.ID, Key: winner.Key, Status: string(winner.Status)}, nil
		}
	}
	return result, err
}
