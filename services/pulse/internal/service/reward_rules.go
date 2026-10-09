package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"math"
	"sort"
	"time"
)

type PublicReward struct {
	Name       string `json:"name"`
	RewardType string `json:"reward_type"`
	Amount     int64  `json:"amount"`
	Weight     uint64 `json:"weight"`
}
type PublicPeriod struct {
	Continuous bool `json:"continuous"`

	ID            uint64    `json:"id"`
	Key           string    `json:"key"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	ConfigVersion string    `json:"config_version"`
}
type RuleBudgetStatus struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Unlimited bool   `json:"unlimited"`
}
type TicketRuleGroup struct {
	RewardRules
	ID               string             `json:"id"`
	TicketCount      int64              `json:"ticket_count"`
	NextLotRemaining int64              `json:"next_lot_remaining"`
	Selection        string             `json:"selection"`
	Budgets          []RuleBudgetStatus `json:"budgets"`
}
type RewardRules struct {
	SelectionVersion int               `json:"selection_version"`
	QueriedAt        time.Time         `json:"queried_at"`
	Groups           []TicketRuleGroup `json:"groups,omitempty"`

	QuotaValidityDays int        `json:"quota_validity_days"`
	QuotaExpiresAt    *time.Time `json:"quota_expires_at,omitempty"`
	ExperienceOnly    bool       `json:"experience_only"`

	QuotaPerUnit      int64          `json:"quota_per_unit"`
	Enabled           bool           `json:"enabled"`
	UnavailableReason string         `json:"unavailable_reason"`
	Period            *PublicPeriod  `json:"period"`
	TicketCost        int            `json:"ticket_cost"`
	Rewards           []PublicReward `json:"rewards"`
	TotalWeight       uint64         `json:"total_weight"`
}
type RewardRulesService struct {
	unit            ports.UnitOfWork
	enabled         bool
	QuotaPerUnit    int64
	SelectionSecret []byte
	now             func() time.Time
}

func NewRewardRulesService(unit ports.UnitOfWork, enabled bool) *RewardRulesService {
	return &RewardRulesService{unit: unit, enabled: enabled, now: time.Now}
}
func (s *RewardRulesService) Get(ctx context.Context) (RewardRules, error) {
	return s.get(ctx, 0)
}
func (s *RewardRulesService) GetForUser(ctx context.Context, userID uint64) (RewardRules, error) {
	return s.get(ctx, userID)
}
func (s *RewardRulesService) get(ctx context.Context, userID uint64) (RewardRules, error) {
	now := s.now()
	result := RewardRules{SelectionVersion: 2, QueriedAt: now, QuotaPerUnit: s.QuotaPerUnit, TicketCost: 1, Rewards: []PublicReward{}, UnavailableReason: "selection_required"}
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Reward == nil {
			return errors.New("reward repositories unavailable")
		}
		if userID == 0 {
			p, err := repos.Period.FindActiveAt(ctx, now)
			if errors.Is(err, period.ErrNoActivePeriod) {
				result.UnavailableReason = "no_active_period"
				return nil
			}
			if err != nil {
				return err
			}
			view, _, err := s.ruleView(ctx, repos, p, nil, now)
			result = view
			// A generic rules read cannot authorize a draw without a personal choice.
			result.Enabled = false
			result.UnavailableReason = "selection_required"
			return err
		}
		if repos.Tickets == nil || repos.Account == nil {
			return errors.New("ticket repositories unavailable")
		}
		if err := repos.Tickets.LockUser(ctx, userID); err != nil {
			return err
		}
		groups, err := repos.Tickets.Groups(ctx, userID, now)
		if err != nil {
			return err
		}
		accounts, err := repos.Account.ListForUser(ctx, userID)
		if err != nil {
			return err
		}
		periods := map[uint64]period.Period{}
		for _, a := range accounts {
			if a.AssetType != ledger.AssetTicket || a.Balance <= 0 {
				continue
			}
			p, err := repos.Tickets.Period(ctx, a.PeriodID)
			if err != nil {
				return err
			}
			periods[p.ID] = p
			if !p.Continuous {
				groups = append(groups, ports.TicketGroup{PeriodID: p.ID, Remaining: a.Balance})
			}
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].PeriodID != groups[j].PeriodID {
				return groups[i].PeriodID > groups[j].PeriodID
			}
			return !groups[i].ExperienceOnly && groups[j].ExperienceOnly
		})
		for _, g := range groups {
			if g.Remaining <= 0 || g.Remaining > (1<<53)-1 {
				return errors.New("invalid ticket group balance")
			}
			p, ok := periods[g.PeriodID]
			if !ok {
				return errors.New("ticket group account missing")
			}
			var lot *ports.TicketLot
			if p.Continuous {
				lot, err = repos.Tickets.NextInGroup(ctx, userID, p.ID, g.ExperienceOnly, now)
				if err != nil {
					return err
				}
				if lot == nil {
					return errors.New("ticket group changed during read")
				}
			}
			view, budgets, err := s.ruleView(ctx, repos, p, lot, now)
			if err != nil {
				return err
			}
			remaining := g.Remaining
			if lot != nil {
				remaining = lot.Remaining
			}
			group := TicketRuleGroup{RewardRules: view, ID: fmt.Sprintf("%d:%t", p.ID, g.ExperienceOnly), TicketCount: g.Remaining, NextLotRemaining: remaining, Budgets: budgets}
			group.Selection = signSelection(s.SelectionSecret, userID, p, lot, g.ExperienceOnly)
			if group.Selection == "" {
				group.Enabled = false
				group.UnavailableReason = "selection_required"
			}
			result.Groups = append(result.Groups, group)
		}
		return nil
	})
	return result, err
}
func (s *RewardRulesService) ruleView(ctx context.Context, repos ports.Repositories, p period.Period, lot *ports.TicketLot, now time.Time) (RewardRules, []RuleBudgetStatus, error) {
	result := RewardRules{SelectionVersion: 2, QueriedAt: now, QuotaPerUnit: s.QuotaPerUnit, TicketCost: 1, Rewards: []PublicReward{}, UnavailableReason: "activity_paused", QuotaValidityDays: p.QuotaValidityDays}
	budgets := []RuleBudgetStatus{}
	if lot != nil {
		result.QuotaExpiresAt = &lot.QuotaExpiresAt
		result.ExperienceOnly = !now.Before(lot.QuotaExpiresAt)
	}
	result.Period = &PublicPeriod{Continuous: p.Continuous, ID: p.ID, Key: p.Key, StartsAt: p.StartsAt, EndsAt: p.EndsAt, ConfigVersion: p.ConfigVersion}
	if p.FundingPolicy != period.VerifiedPaidFunding || p.TicketThresholdMilli <= 0 {
		result.UnavailableReason = "funding_verification_required"
		return result, budgets, nil
	}
	defs, err := repos.Reward.ListDefinitions(ctx, p.ID)
	if err != nil {
		return result, budgets, err
	}
	if err = validateRewardDefinitions(defs, p.ConfigVersion); err != nil {
		return result, budgets, err
	}
	if result.ExperienceOnly {
		defs = experienceOnly(defs)
	}
	for _, d := range defs {
		if !d.Enabled {
			continue
		}
		if (d.RewardType != "newapi_quota" && d.RewardType != ExperienceRewardType) || d.Weight > (1<<53)-1 || d.Amount > (1<<53)-1 || math.MaxUint64-result.TotalWeight < d.Weight {
			return result, budgets, errors.New("unsupported public reward")
		}
		result.TotalWeight += d.Weight
		if result.TotalWeight > (1<<53)-1 {
			return result, budgets, errors.New("public probability weight overflow")
		}
		result.Rewards = append(result.Rewards, PublicReward{Name: d.RewardKey, RewardType: d.RewardType, Amount: d.Amount, Weight: d.Weight})
	}
	poolAvailable := len(result.Rewards) > 0
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
		b, err := repos.Reward.GetBudget(ctx, p.ID, kind)
		state := RuleBudgetStatus{ID: fmt.Sprintf("%d:%s", p.ID, kind), Kind: kind}
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return result, budgets, err
		}
		if err == nil {
			state.Available = b.CanReserve(largest)
			state.Unlimited = b.Unlimited
		}
		budgets = append(budgets, state)
		poolAvailable = poolAvailable && state.Available
	}
	switch {
	case !s.enabled:
		result.UnavailableReason = "activity_paused"
	case p.Status != period.StatusActive || !p.Contains(now):
		result.UnavailableReason = "rule_inactive"
	case !poolAvailable:
		result.UnavailableReason = "budget_exhausted"
	default:
		result.Enabled = true
		result.UnavailableReason = ""
	}
	return result, budgets, nil
}
