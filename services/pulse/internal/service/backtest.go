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

var ErrBacktestOverflow = errors.New("backtest integer overflow")

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
	seenConsumes := make(map[string]usage.Event)
	after := ""
	for {
		pageStart := after
		events, err := s.source.Fetch(ctx, after, s.cfg.BatchSize)
		if err != nil {
			return report, err
		}
		if len(events) == 0 {
			return s.finish(report, users, nets)
		}
		for _, event := range events {
			report.Fetched++
			after = event.CursorValue
			// Keep consume metadata before the range filter. A refund inside the
			// selected window may legitimately point to a consume before --from;
			// the correlation must still be checked rather than assumed.
			if event.EventType == usage.EventConsume {
				seenConsumes[consumeKey(event.UserID, event.SourceEventID)] = event
				if event.RequestID != "" {
					seenRequests[requestKey(event.UserID, event.RequestID)] = event
				}
			}
			if !to.IsZero() && !event.SourceCreatedAt.Before(to) {
				return s.finish(report, users, nets)
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
			if err := s.evaluate(ctx, event, seenRequests, seenConsumes, &report, nets); err != nil {
				return report, err
			}
		}
		if after == pageStart {
			return report, errors.New("backtest source did not advance cursor")
		}
	}
}

func (s *BacktestService) finish(report BacktestReport, users map[uint64]struct{}, nets map[string]int64) (BacktestReport, error) {
	report.UniqueUsers = len(users)
	report.CoverageBps = ratioBps(report.EligibleEvents, report.InRange)
	report.AnomalyBps = ratioBps(report.ManualReviewEvents+report.NoActivePeriodEvents+report.NoMatchingRuleEvents+report.RefundCorrelationGaps, report.InRange)
	finalTickets, err := finalTickets(nets, s.cfg.TicketThresholdMilli)
	if err != nil {
		return report, err
	}
	report.FinalTicketEntitlement = finalTickets
	return report, nil
}

func (s *BacktestService) evaluate(ctx context.Context, event usage.Event, seenRequests, seenConsumes map[string]usage.Event, report *BacktestReport, nets map[string]int64) error {
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
		if err := economics.ValidateRules(rules, activity.ConfigVersion); err != nil {
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
		if event.EventType == usage.EventRefund && !refundCorrelationMatches(ctx, repos, event, activity.ID, seenRequests, seenConsumes) {
			report.RefundCorrelationGaps++
			report.DataGaps["refund consume correlation unavailable"]++
			return nil
		}
		if decision.Eligible {
			report.EligibleEvents++
			var err error
			report.NetContributionMilli, err = addBacktestInt64(report.NetContributionMilli, int64(decision.Contribution), "net contribution")
			if err != nil {
				return err
			}
			key := fmt.Sprintf("%d:%d", event.UserID, activity.ID)
			nets[key], err = addBacktestInt64(nets[key], int64(decision.Contribution), "user-period contribution")
			if err != nil {
				return err
			}
			// quota is a user-side estimate, not provider cost. A refund reduces
			// the estimate; provider cost remains unavailable from LOG_DB.
			report.EstimatedCostQuota, err = addBacktestInt64(report.EstimatedCostQuota, event.QuotaDelta, "estimated cost quota")
			if err != nil {
				return err
			}
			manual, err := contributionWithMultiplier(event.QuotaDelta, s.cfg.ManualMultiplierBps)
			if err != nil {
				return fmt.Errorf("manual multiplier comparison: %w", err)
			}
			model, err := compareRule(event, rules, true)
			if err != nil {
				return fmt.Errorf("model multiplier comparison: %w", err)
			}
			channel, err := compareRule(event, rules, false)
			if err != nil {
				return fmt.Errorf("channel multiplier comparison: %w", err)
			}
			report.Comparison.ManualContributionMilli, err = addBacktestInt64(report.Comparison.ManualContributionMilli, manual, "manual comparison contribution")
			if err != nil {
				return err
			}
			report.Comparison.ModelContributionMilli, err = addBacktestInt64(report.Comparison.ModelContributionMilli, model, "model comparison contribution")
			if err != nil {
				return err
			}
			report.Comparison.ChannelContributionMilli, err = addBacktestInt64(report.Comparison.ChannelContributionMilli, channel, "channel comparison contribution")
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func compareRule(event usage.Event, rules []economics.Rule, byModel bool) (int64, error) {
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
		return 0, nil
	}
	return contributionWithMultiplier(event.QuotaDelta, rule.MultiplierBps)
}

func contributionWithMultiplier(quota int64, multiplier money.Bps) (int64, error) {
	value, err := money.Milli(quota).MultiplyBps(multiplier)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrBacktestOverflow, err)
	}
	return int64(value), nil
}

func finalTickets(nets map[string]int64, threshold int64) (int64, error) {
	var total int64
	for _, net := range nets {
		if net <= 0 {
			continue
		}
		if net/threshold > math.MaxInt64-total {
			return 0, fmt.Errorf("%w: final ticket entitlement", ErrBacktestOverflow)
		}
		total += net / threshold
	}
	return total, nil
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

func consumeKey(userID uint64, sourceEventID string) string {
	return fmt.Sprintf("%d:%s", userID, sourceEventID)
}

// refundCorrelationMatches mirrors the ingest safety rule in read-only
// backtests: a refund must resolve to a consume for the same user and the
// same period. A supplied origin id takes precedence over request_id.
func refundCorrelationMatches(ctx context.Context, repos ports.Repositories, event usage.Event, periodID uint64, seenRequests, seenConsumes map[string]usage.Event) bool {
	var original *usage.Event
	if event.RelatedSourceEventID != "" {
		if found, ok := seenConsumes[consumeKey(event.UserID, event.RelatedSourceEventID)]; ok {
			original = &found
		} else if repos.Usage != nil {
			found, err := repos.Usage.FindBySource(ctx, event.SourceSystem, event.RelatedSourceEventID)
			if err == nil && found != nil {
				original = found
			}
		}
	} else if event.RequestID != "" {
		if found, ok := seenRequests[requestKey(event.UserID, event.RequestID)]; ok {
			original = &found
		} else if repos.Usage != nil {
			found, err := repos.Usage.FindConsumeByRequest(ctx, event.UserID, event.RequestID)
			if err == nil && found != nil {
				original = found
			}
		}
	}
	if original == nil || original.EventType != usage.EventConsume || original.NeedsReview || original.Status == usage.StatusManualReview || original.UserID != event.UserID {
		return false
	}
	if original.PeriodID != 0 {
		return original.PeriodID == periodID
	}
	activity, err := repos.Period.FindActiveAt(ctx, original.SourceCreatedAt)
	return err == nil && activity.ID == periodID
}

func addBacktestInt64(left, right int64, field string) (int64, error) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, fmt.Errorf("%w: %s", ErrBacktestOverflow, field)
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, fmt.Errorf("%w: %s", ErrBacktestOverflow, field)
	}
	return left + right, nil
}
