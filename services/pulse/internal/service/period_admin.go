package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

var ErrInvalidPeriod = errors.New("invalid period")
var webPeriodKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// The web form always creates a complete, frozen paid-funding period. It
// cannot edit active economics or switch on settlement.
type PeriodAdminRequest struct {
	Key                  string             `json:"key"`
	StartsAt             time.Time          `json:"starts_at"`
	MultiplierBps        int32              `json:"multiplier_bps"`
	TicketThresholdMilli int64              `json:"ticket_threshold_milli"`
	RewardBudget         int64              `json:"reward_budget"`
	Rewards              []PeriodRewardSpec `json:"rewards"`
	Reason               string             `json:"reason"`
}

func (s *PeriodCreateService) CreateFromAdmin(ctx context.Context, request PeriodAdminRequest, actor, key string) (PeriodCreateResult, error) {
	if !webPeriodKey.MatchString(request.Key) || !webPeriodKey.MatchString(key) || actor == "" ||
		request.StartsAt.IsZero() || request.StartsAt.Year() < 2000 || request.StartsAt.Year() > 9998 ||
		request.MultiplierBps <= 0 || money.Bps(request.MultiplierBps) > money.MaxBps ||
		request.TicketThresholdMilli <= 0 || request.TicketThresholdMilli > int64(maxPublicRewardInteger) ||
		request.RewardBudget <= 0 || request.RewardBudget > int64(maxPublicRewardInteger) || len(request.Rewards) == 0 ||
		utf8.RuneCountInString(strings.TrimSpace(request.Reason)) < 3 || utf8.RuneCountInString(request.Reason) > 500 {
		return PeriodCreateResult{}, ErrInvalidPeriod
	}
	return s.Create(ctx, PeriodCreateCommand{
		RequestID: key, ActorType: "community_admin", ActorID: actor,
		Key: request.Key, StartsAt: request.StartsAt.UTC(), Timezone: "Asia/Shanghai",
		ConfigVersion: request.Key, RandomVersion: request.Key,
		Rules:                []PeriodRuleSpec{{Key: "default", Eligible: true, MultiplierBps: request.MultiplierBps}},
		TicketThresholdMilli: request.TicketThresholdMilli, RewardBudget: request.RewardBudget,
		Rewards: request.Rewards, Activate: true, Reason: request.Reason,
	})
}

type AdminPeriodView struct {
	ID                   uint64            `json:"id"`
	Key                  string            `json:"key"`
	Status               string            `json:"status"`
	StartsAt             time.Time         `json:"starts_at"`
	EndsAt               time.Time         `json:"ends_at"`
	TicketThresholdMilli int64             `json:"ticket_threshold_milli"`
	Rules                []AdminPeriodRule `json:"rules"`
}
type AdminPeriodRule struct {
	Key           string `json:"key"`
	MultiplierBps int32  `json:"multiplier_bps"`
}

func (s *PeriodCreateService) ListForAdmin(ctx context.Context) ([]AdminPeriodView, error) {
	result := make([]AdminPeriodView, 0)
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Overview == nil || repos.PeriodAdmin == nil {
			return errors.New("period overview unavailable")
		}
		periods, err := repos.Overview.ListPeriods(ctx, 20)
		if err != nil {
			return err
		}
		for _, row := range periods {
			activity, err := repos.PeriodAdmin.FindByIDForUpdate(ctx, row.ID)
			if err != nil {
				return err
			}
			rules, err := repos.Overview.ListRules(ctx, row.ID)
			if err != nil {
				return err
			}
			view := AdminPeriodView{ID: row.ID, Key: row.Key, Status: row.Status, StartsAt: row.StartsAt, EndsAt: row.EndsAt, TicketThresholdMilli: activity.TicketThresholdMilli, Rules: make([]AdminPeriodRule, 0)}
			for _, rule := range rules {
				view.Rules = append(view.Rules, AdminPeriodRule{Key: rule.RuleKey, MultiplierBps: rule.MultiplierBps})
			}
			result = append(result, view)
		}
		return nil
	})
	return result, err
}
