package service

import (
	"context"
	"errors"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"math"
	"time"
)

type PublicReward struct {
	Name       string `json:"name"`
	RewardType string `json:"reward_type"`
	Amount     int64  `json:"amount"`
	Weight     uint64 `json:"weight"`
}
type PublicPeriod struct {
	ID            uint64    `json:"id"`
	Key           string    `json:"key"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	ConfigVersion string    `json:"config_version"`
}
type RewardRules struct {
	QuotaPerUnit      int64          `json:"quota_per_unit"`
	Enabled           bool           `json:"enabled"`
	UnavailableReason string         `json:"unavailable_reason"`
	Period            *PublicPeriod  `json:"period"`
	TicketCost        int            `json:"ticket_cost"`
	Rewards           []PublicReward `json:"rewards"`
	TotalWeight       uint64         `json:"total_weight"`
}
type RewardRulesService struct {
	unit         ports.UnitOfWork
	enabled      bool
	QuotaPerUnit int64
	now          func() time.Time
}

func NewRewardRulesService(unit ports.UnitOfWork, enabled bool) *RewardRulesService {
	return &RewardRulesService{unit: unit, enabled: enabled, now: time.Now}
}
func (s *RewardRulesService) Get(ctx context.Context) (RewardRules, error) {
	result := RewardRules{QuotaPerUnit: s.QuotaPerUnit, TicketCost: 1, Rewards: []PublicReward{}, UnavailableReason: "activity_paused"}
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Reward == nil {
			return errors.New("reward repositories unavailable")
		}
		p, err := repos.Period.FindActiveAt(ctx, s.now())
		if errors.Is(err, period.ErrNoActivePeriod) {
			result.UnavailableReason = "no_active_period"
			return nil
		}
		if err != nil {
			return err
		}
		result.Period = &PublicPeriod{ID: p.ID, Key: p.Key, StartsAt: p.StartsAt, EndsAt: p.EndsAt, ConfigVersion: p.ConfigVersion}
		if p.FundingPolicy != period.VerifiedPaidFunding || p.TicketThresholdMilli <= 0 {
			result.UnavailableReason = "funding_verification_required"
			return nil
		}
		defs, err := repos.Reward.ListDefinitions(ctx, p.ID)
		if err != nil {
			return err
		}
		if err := validateRewardDefinitions(defs, p.ConfigVersion); err != nil {
			return err
		}
		for _, d := range defs {
			if !d.Enabled {
				continue
			}
			if (d.RewardType != "newapi_quota" && d.RewardType != ExperienceRewardType) || d.Weight > (1<<53)-1 || d.Amount > (1<<53)-1 || math.MaxUint64-result.TotalWeight < d.Weight {
				return errors.New("unsupported public reward")
			}
			result.TotalWeight += d.Weight
			if result.TotalWeight > (1<<53)-1 {
				return errors.New("public probability weight overflow")
			}
			result.Rewards = append(result.Rewards, PublicReward{Name: d.RewardKey, RewardType: d.RewardType, Amount: d.Amount, Weight: d.Weight})
		}
		if len(result.Rewards) == 0 {
			result.UnavailableReason = "reward_pool_unavailable"
			return nil
		}

		for _, kind := range []string{ActionBudgetType, ExperienceRewardType} {
			var largest int64
			for _, d := range result.Rewards {
				if rewardBudget(d.RewardType) == kind && d.Amount > largest {
					largest = d.Amount
				}
			}
			if largest == 0 {
				continue
			}
			budget, err := repos.Reward.GetBudgetForUpdate(ctx, p.ID, kind)
			if errors.Is(err, ports.ErrNotFound) {
				result.UnavailableReason = "reward_pool_unavailable"
				return nil
			}
			if err != nil {
				return err
			}
			if budget.SettledAmount < 0 || budget.ReservedAmount < 0 || budget.HardCap < budget.SettledAmount || budget.ReservedAmount > budget.HardCap-budget.SettledAmount || budget.HardCap-budget.SettledAmount-budget.ReservedAmount < largest {
				result.UnavailableReason = "budget_exhausted"
				return nil
			}
		}

		if s.enabled {
			result.Enabled = true
			result.UnavailableReason = ""
		}
		return nil
	})
	return result, err
}
