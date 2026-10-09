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
type DrawChoice struct {
	Selection      string    `json:"selection"`
	ExperienceOnly bool      `json:"experience_only"`
	QuotaExpiresAt time.Time `json:"quota_expires_at"`
}
type RewardRules struct {
	SelectionVersion      int                `json:"selection_version"`
	QueriedAt             time.Time          `json:"queried_at"`
	TicketCount           int64              `json:"ticket_count"`
	Selection             string             `json:"selection,omitempty"`
	Draws                 []DrawChoice       `json:"draws"`
	CanDrawFive           bool               `json:"can_draw_five"`
	Budgets               []RuleBudgetStatus `json:"budgets"`
	ExperienceRewards     []PublicReward     `json:"experience_rewards"`
	ExperienceTotalWeight uint64             `json:"experience_total_weight"`

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
	result := RewardRules{SelectionVersion: 3, QueriedAt: now, QuotaPerUnit: s.QuotaPerUnit, TicketCost: 1, Rewards: []PublicReward{}, UnavailableReason: "no_active_period"}
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Reward == nil {
			return errors.New("reward repositories unavailable")
		}
		current, err := repos.Period.FindActiveAt(ctx, now)
		if errors.Is(err, period.ErrNoActivePeriod) {
			return nil
		}
		if err != nil {
			return err
		}
		result, result.Budgets, err = s.ruleView(ctx, repos, current, nil, now)
		if err != nil {
			return err
		}
		if userID == 0 {
			result.Enabled = false
			result.UnavailableReason = "selection_required"
			return nil
		}
		if repos.Tickets == nil || repos.Account == nil {
			return errors.New("ticket repositories unavailable")
		}
		// All preview reads share the same DB snapshot. No ticket or budget is reserved.
		accounts, err := repos.Account.ListForUser(ctx, userID)
		if err != nil {
			return err
		}
		type candidate struct {
			source          period.Period
			lot             *ports.TicketLot
			remaining       int64
			earned, expires time.Time
		}
		candidates := []candidate{}
		sources := map[uint64]period.Period{}
		var total int64
		for _, account := range accounts {
			if account.AssetType != ledger.AssetTicket || account.Balance <= 0 {
				continue
			}
			source, err := repos.Tickets.Period(ctx, account.PeriodID)
			if err != nil {
				return err
			}
			if source.FundingPolicy != period.VerifiedPaidFunding || source.TicketThresholdMilli <= 0 {
				continue
			}
			if !source.Continuous && (source.Status != period.StatusActive || !source.Contains(now)) {
				continue
			}
			if account.Balance > (1<<53)-1-total {
				return errors.New("public ticket balance overflow")
			}
			total += account.Balance
			sources[source.ID] = source
			if !source.Continuous {
				candidates = append(candidates, candidate{source: source, remaining: account.Balance, earned: source.StartsAt, expires: source.EndsAt})
			}
		}
		lots, err := repos.Tickets.PreviewLots(ctx, userID, now)
		if err != nil {
			return err
		}
		for i := range lots {
			lot := &lots[i]
			source, ok := sources[lot.PeriodID]
			if !ok {
				return errors.New("ticket source account missing")
			}
			candidates = append(candidates, candidate{source: source, lot: lot, remaining: lot.Remaining, earned: lot.EarnedAt, expires: lot.QuotaExpiresAt})
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			ae, be := !now.Before(a.expires), !now.Before(b.expires)
			if ae != be {
				return !ae
			}
			if !a.earned.Equal(b.earned) {
				return a.earned.Before(b.earned)
			}
			if a.source.ID != b.source.ID {
				return a.source.ID < b.source.ID
			}
			if a.lot != nil && b.lot != nil {
				return a.lot.ID < b.lot.ID
			}
			return false
		})
		draws := []DrawChoice{}
		for _, c := range candidates {
			exp := !now.Before(c.expires)
			token := signSelection(s.SelectionSecret, userID, current, c.lot, exp, c.source.ID)
			for n := int64(0); n < c.remaining && len(draws) < 5; n++ {
				draws = append(draws, DrawChoice{Selection: token, ExperienceOnly: exp, QuotaExpiresAt: c.expires})
			}
			if len(draws) == 5 {
				break
			}
		}
		if len(draws) > 0 {
			// The first choice controls single-draw odds; the EXP table also describes
			// any expired choices later in the five-draw plan.
			viewLot := &ports.TicketLot{QuotaExpiresAt: draws[0].QuotaExpiresAt}
			result, result.Budgets, err = s.ruleView(ctx, repos, current, viewLot, now)
			if err != nil {
				return err
			}
			result.QuotaExpiresAt = &draws[0].QuotaExpiresAt
			result.Selection = draws[0].Selection
		}
		expired := &ports.TicketLot{QuotaExpiresAt: now}
		expView, _, err := s.ruleView(ctx, repos, current, expired, now)
		if err != nil {
			return err
		}
		result.ExperienceRewards = expView.Rewards
		result.ExperienceTotalWeight = expView.TotalWeight
		result.TicketCount = total
		result.Draws = draws
		result.CanDrawFive = result.Enabled && len(draws) == 5
		for _, draw := range draws {
			if draw.ExperienceOnly && !expView.Enabled {
				result.CanDrawFive = false
			}
		}
		if len(draws) == 0 {
			result.Enabled = false
			result.UnavailableReason = "insufficient_tickets"
		}
		if len(s.SelectionSecret) == 0 {
			result.Enabled = false
			result.CanDrawFive = false
			result.UnavailableReason = "selection_required"
		}
		return nil
	})
	return result, err
}
func (s *RewardRulesService) ruleView(ctx context.Context, repos ports.Repositories, p period.Period, lot *ports.TicketLot, now time.Time) (RewardRules, []RuleBudgetStatus, error) {
	result := RewardRules{SelectionVersion: 3, QueriedAt: now, QuotaPerUnit: s.QuotaPerUnit, TicketCost: 1, Rewards: []PublicReward{}, UnavailableReason: "activity_paused", QuotaValidityDays: p.QuotaValidityDays}
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
