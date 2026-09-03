package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type BacktestConfig struct {
	BatchSize            int
	TicketThresholdMilli int64
	ManualMultiplierBps  money.Bps
}

type BacktestReport struct {
	From                   time.Time        `json:"from"`
	To                     time.Time        `json:"to"`
	Fetched                int              `json:"fetched"`
	InRange                int              `json:"in_range"`
	UniqueUsers            int              `json:"unique_users"`
	ConsumeEvents          int              `json:"consume_events"`
	RefundEvents           int              `json:"refund_events"`
	EligibleEvents         int              `json:"eligible_events"`
	ManualReviewEvents     int              `json:"manual_review_events"`
	NoActivePeriodEvents   int              `json:"no_active_period_events"`
	NoMatchingRuleEvents   int              `json:"no_matching_rule_events"`
	RefundCorrelationGaps  int              `json:"refund_correlation_gaps"`
	NetContributionMilli   int64            `json:"net_contribution_milli"`
	FinalTicketEntitlement int64            `json:"final_ticket_entitlement"`
	CoverageBps            int64            `json:"coverage_bps"`
	AnomalyBps             int64            `json:"anomaly_bps"`
	EstimatedCostQuota     int64            `json:"estimated_cost_quota"`
	ProviderCostAvailable  bool             `json:"provider_cost_available"`
	MarginFactSource       string           `json:"margin_fact_source"`
	Comparison             MultiplierReport `json:"comparison"`
	DataGaps               map[string]int   `json:"data_gaps,omitempty"`
}

type MultiplierReport struct {
	ManualContributionMilli  int64 `json:"manual_contribution_milli"`
	ModelContributionMilli   int64 `json:"model_contribution_milli"`
	ChannelContributionMilli int64 `json:"channel_contribution_milli"`
}

type BacktestService struct {
	unit   ports.UnitOfWork
	source ports.UsageSource
	cfg    BacktestConfig
}

func NewBacktestService(unit ports.UnitOfWork, source ports.UsageSource, cfg BacktestConfig) (*BacktestService, error) {
	if unit == nil || source == nil {
		return nil, errors.New("backtest dependencies are nil")
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 5000 {
		return nil, errors.New("backtest batch size must be between 1 and 5000")
	}
	if cfg.TicketThresholdMilli <= 0 {
		return nil, errors.New("backtest ticket threshold must be positive")
	}
	if err := cfg.ManualMultiplierBps.Validate(); err != nil {
		return nil, err
	}
	return &BacktestService{unit: unit, source: source, cfg: cfg}, nil
}

// Run performs a read-only replay. It never calls UsageRepository.Create or
// any ledger/account mutation, so operators can run it against production
// replicas before changing a campaign parameter. The range is half-open:
// [from, to), matching Period semantics.
func (s *BacktestService) Run(ctx context.Context, from, to time.Time) (BacktestReport, error) {
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return BacktestReport{}, errors.New("backtest to time is before from time")
	}
	report := BacktestReport{From: from, To: to, DataGaps: make(map[string]int), MarginFactSource: "estimated_user_quota", ProviderCostAvailable: false}
	users := make(map[uint64]struct{})
	nets := make(map[string]int64)
	seenRequests := make(map[string]usage.Event)
	after := ""
	for {
		pageStart := after
		events, err := s.source.Fetch(ctx, after, s.cfg.BatchSize)
		if err != nil {
			return report, err
		}
		if len(events) == 0 {
			return s.finish(report, users, nets), nil
		}
		for _, event := range events {
			report.Fetched++
			after = event.CursorValue
			if !to.IsZero() && !event.SourceCreatedAt.Before(to) {
				return s.finish(report, users, nets), nil
			}
			if !from.IsZero() && event.SourceCreatedAt.Before(from) {
				continue
			}
			report.InRange++
			users[event.UserID] = struct{}{}
			switch event.EventType {
			case usage.EventConsume:
				report.ConsumeEvents++
			case usage.EventRefund:
				report.RefundEvents++
			}
			if event.NeedsReview {
				report.ManualReviewEvents++
				report.DataGaps[event.ReviewReason]++
				continue
			}
			if event.EventType == usage.EventRefund && event.RelatedSourceEventID == "" && event.RequestID == "" {
				report.RefundCorrelationGaps++
				report.DataGaps["refund has no stable consume correlation"]++
				continue
			}
			if event.RequestID != "" && event.EventType == usage.EventConsume {
				seenRequests[requestKey(event.UserID, event.RequestID)] = event
			}
			if err := s.evaluate(ctx, event, seenRequests, &report, nets); err != nil {
				return report, err
			}
		}
		if after == pageStart {
			return report, errors.New("backtest source did not advance cursor")
		}
	}
}

func (s *BacktestService) finish(report BacktestReport, users map[uint64]struct{}, nets map[string]int64) BacktestReport {
	report.UniqueUsers = len(users)
	report.CoverageBps = ratioBps(report.EligibleEvents, report.InRange)
	report.AnomalyBps = ratioBps(report.ManualReviewEvents+report.NoActivePeriodEvents+report.NoMatchingRuleEvents+report.RefundCorrelationGaps, report.InRange)
	report.FinalTicketEntitlement = finalTickets(nets, s.cfg.TicketThresholdMilli)
	return report
}

func (s *BacktestService) evaluate(ctx context.Context, event usage.Event, seenRequests map[string]usage.Event, report *BacktestReport, nets map[string]int64) error {
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Period == nil || repos.Economics == nil {
			return errors.New("backtest read repositories are not initialized")
		}
		activity, err := repos.Period.FindActiveAt(ctx, event.SourceCreatedAt)
		if err != nil {
			report.NoActivePeriodEvents++
			report.DataGaps["no active period"]++
			return nil
		}
		rules, err := repos.Economics.ListRules(ctx, activity.ID)
		if err != nil {
			return err
		}
		rule, found := economics.Select(rules, event.ModelName, event.ChannelID)
		if !found {
			report.NoMatchingRuleEvents++
			report.DataGaps["no matching economics rule"]++
			return nil
		}
		decision, err := economics.Evaluate(event.QuotaDelta, rule)
		if err != nil {
			return err
		}
		if event.EventType == usage.EventRefund {
			if event.RelatedSourceEventID == "" && event.RequestID != "" {
				if _, ok := seenRequests[requestKey(event.UserID, event.RequestID)]; !ok && repos.Usage != nil {
					if _, lookupErr := repos.Usage.FindConsumeByRequest(ctx, event.UserID, event.RequestID); lookupErr != nil {
						report.RefundCorrelationGaps++
						report.DataGaps["refund consume correlation unavailable"]++
						return nil
					}
				}
			}
		}
		if decision.Eligible {
			report.EligibleEvents++
			report.NetContributionMilli = addInt64(report.NetContributionMilli, int64(decision.Contribution))
			key := fmt.Sprintf("%d:%d", event.UserID, activity.ID)
			nets[key] = addInt64(nets[key], int64(decision.Contribution))
			// quota is a user-side estimate, not provider cost. A refund reduces
			// the estimate; provider cost remains unavailable from LOG_DB.
			report.EstimatedCostQuota = addInt64(report.EstimatedCostQuota, event.QuotaDelta)
			report.Comparison.ManualContributionMilli = addInt64(report.Comparison.ManualContributionMilli, contributionWithMultiplier(event.QuotaDelta, s.cfg.ManualMultiplierBps))
			report.Comparison.ModelContributionMilli = addInt64(report.Comparison.ModelContributionMilli, compareRule(event, rules, true))
			report.Comparison.ChannelContributionMilli = addInt64(report.Comparison.ChannelContributionMilli, compareRule(event, rules, false))
		}
		return nil
	})
}

func compareRule(event usage.Event, rules []economics.Rule, byModel bool) int64 {
	candidates := make([]economics.Rule, 0, len(rules))
	for _, candidate := range rules {
		if byModel {
			// Model comparison intentionally excludes channel overrides; otherwise
			// removing ChannelID would turn a channel rule into a false default.
			if candidate.ChannelID != nil {
				continue
			}
		} else if candidate.ModelPattern != "" {
			// Channel comparison likewise excludes model overrides.
			continue
		}
		candidates = append(candidates, candidate)
	}
	rule, ok := economics.Select(candidates, event.ModelName, event.ChannelID)
	if !ok || !rule.Eligible {
		return 0
	}
	return contributionWithMultiplier(event.QuotaDelta, rule.MultiplierBps)
}

func contributionWithMultiplier(quota int64, multiplier money.Bps) int64 {
	value, err := money.Milli(quota).MultiplyBps(multiplier)
	if err != nil {
		return 0
	}
	return int64(value)
}

func finalTickets(nets map[string]int64, threshold int64) int64 {
	var total int64
	for _, net := range nets {
		if net <= 0 {
			continue
		}
		if net/threshold > math.MaxInt64-total {
			return math.MaxInt64
		}
		total += net / threshold
	}
	return total
}

func ratioBps(numerator, denominator int) int64 {
	if denominator <= 0 {
		return 0
	}
	return int64(numerator) * 10000 / int64(denominator)
}

func requestKey(userID uint64, requestID string) string {
	return fmt.Sprintf("%d:%s", userID, requestID)
}

func addInt64(left, right int64) int64 {
	if right > 0 && left > math.MaxInt64-right {
		return math.MaxInt64
	}
	if right < 0 && left < math.MinInt64-right {
		return math.MinInt64
	}
	return left + right
}
