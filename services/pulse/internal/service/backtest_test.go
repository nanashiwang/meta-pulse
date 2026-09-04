package service

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type backtestSource struct {
	events []usage.Event
}

func (s backtestSource) Fetch(_ context.Context, after string, limit int) ([]usage.Event, error) {
	start := 0
	if after != "" {
		for start < len(s.events) && s.events[start].CursorValue != after {
			start++
		}
		if start < len(s.events) {
			start++
		}
	}
	if start >= len(s.events) {
		return nil, nil
	}
	end := start + limit
	if end > len(s.events) {
		end = len(s.events)
	}
	return append([]usage.Event(nil), s.events[start:end]...), nil
}

type stuckBacktestSource struct{ event usage.Event }

func (s stuckBacktestSource) Fetch(context.Context, string, int) ([]usage.Event, error) {
	return []usage.Event{s.event}, nil
}

func newBacktest(t *testing.T, store *memoryLedgerStore, source ports.UsageSource) *BacktestService {
	t.Helper()
	result, err := NewBacktestService(memoryUnit{store: store}, source, BacktestConfig{
		BatchSize:            2,
		TicketThresholdMilli: 1000,
		ManualMultiplierBps:  10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func backtestEvent(id string, at time.Time, eventType usage.EventType, quota int64) usage.Event {
	return usage.Event{
		SourceSystem: "new-api-log", SourceEventID: id, CursorValue: id, PayloadHash: id,
		UserID: 9, EventType: eventType, SourceCreatedAt: at, QuotaDelta: quota,
		ModelName: "gpt-4o", ChannelID: 2,
	}
}

func TestBacktestIsReadOnlyAndUsesHalfOpenRange(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	events := []usage.Event{
		backtestEvent("before", at.Add(-time.Minute), usage.EventConsume, 100),
		backtestEvent("inside", at, usage.EventConsume, 1200),
		backtestEvent("at-to", at.Add(time.Minute), usage.EventConsume, 900),
	}
	report, err := newBacktest(t, store, backtestSource{events: events}).Run(context.Background(), at.Add(-time.Second), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if report.Fetched != 3 || report.InRange != 1 || report.EligibleEvents != 1 || report.UniqueUsers != 1 {
		t.Fatalf("unexpected range report: %+v", report)
	}
	if report.NetContributionMilli != 1200 || report.FinalTicketEntitlement != 1 || report.EstimatedCostQuota != 1200 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if len(store.usageEvents) != 0 || len(store.entries) != 0 || len(store.accounts) != 0 {
		t.Fatalf("backtest mutated pulse state: usage=%d ledger=%d accounts=%d", len(store.usageEvents), len(store.entries), len(store.accounts))
	}
}

func TestBacktestReportsPeriodRuleAndRefundGaps(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "gpt-only", ModelPattern: "gpt-*", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	events := []usage.Event{
		backtestEvent("no-period", at.Add(-2*time.Hour), usage.EventConsume, 100),
		backtestEvent("no-rule", at, usage.EventConsume, 100),
		backtestEvent("gap", at.Add(time.Minute), usage.EventRefund, -100),
	}
	events[1].ModelName = "claude-3"
	events[2].RequestID = "missing-request"
	report, err := newBacktest(t, store, backtestSource{events: events}).Run(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.NoActivePeriodEvents != 1 || report.NoMatchingRuleEvents != 1 || report.RefundCorrelationGaps != 1 {
		t.Fatalf("unexpected anomaly report: %+v", report)
	}
	if report.EligibleEvents != 0 || report.NetContributionMilli != 0 {
		t.Fatalf("unexpected eligible totals: %+v", report)
	}
	if report.DataGaps["no active period"] != 1 || report.DataGaps["no matching economics rule"] != 1 {
		t.Fatalf("missing data gap details: %+v", report.DataGaps)
	}
}

func TestBacktestValidatesDirectRefundCorrelation(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	events := []usage.Event{
		backtestEvent("consume", at, usage.EventConsume, 1000),
		backtestEvent("refund", at.Add(time.Minute), usage.EventRefund, -100),
	}
	events[0].RequestID = "request-1"
	events[1].RelatedSourceEventID = "consume"
	report, err := newBacktest(t, store, backtestSource{events: events}).Run(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.RefundCorrelationGaps != 0 || report.EligibleEvents != 2 || report.NetContributionMilli != 900 {
		t.Fatalf("valid direct refund was not accepted: %+v", report)
	}
}

func TestBacktestRejectsRefundLinkedToManualReviewConsume(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	store.usageEvents = []usage.Event{{
		SourceSystem: "new-api-log", SourceEventID: "manual-consume", UserID: 9, PeriodID: 4,
		EventType: usage.EventConsume, SourceCreatedAt: at, Status: usage.StatusManualReview,
	}}
	event := backtestEvent("refund", at.Add(time.Minute), usage.EventRefund, -100)
	event.RelatedSourceEventID = "manual-consume"
	report, err := newBacktest(t, store, backtestSource{events: []usage.Event{event}}).Run(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.RefundCorrelationGaps != 1 || report.EligibleEvents != 0 {
		t.Fatalf("manual-review consume was accepted as refund origin: %+v", report)
	}
}

func TestBacktestRejectsUnknownDirectRefundCorrelation(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	event := backtestEvent("refund", at, usage.EventRefund, -100)
	event.RelatedSourceEventID = "missing-consume"
	report, err := newBacktest(t, store, backtestSource{events: []usage.Event{event}}).Run(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.RefundCorrelationGaps != 1 || report.EligibleEvents != 0 || report.NetContributionMilli != 0 {
		t.Fatalf("unknown direct refund was accepted: %+v", report)
	}
}

func TestBacktestComparesMultipliers(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	channel := uint64(2)
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{
		{ID: 1, Key: "default", Priority: 0, Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"},
		{ID: 2, Key: "model", Priority: 10, ModelPattern: "gpt-*", Eligible: true, MultiplierBps: 20000, ConfigVersion: "v1"},
		{ID: 3, Key: "channel", Priority: 20, ChannelID: &channel, Eligible: true, MultiplierBps: 30000, ConfigVersion: "v1"},
	}
	event := backtestEvent("1", at, usage.EventConsume, 1000)
	report, err := newBacktest(t, store, backtestSource{events: []usage.Event{event}}).Run(context.Background(), time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Comparison.ManualContributionMilli != 1000 || report.Comparison.ModelContributionMilli != 2000 || report.Comparison.ChannelContributionMilli != 3000 {
		t.Fatalf("comparison=%+v", report.Comparison)
	}
}

func TestBacktestFailsWhenSourceCursorDoesNotAdvance(t *testing.T) {
	store := newMemoryLedgerStore()
	at := time.Unix(1_700_000_000, 0).UTC()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 1, Key: "default", Eligible: true, MultiplierBps: money.Bps(10000), ConfigVersion: "v1"}}
	_, err := newBacktest(t, store, stuckBacktestSource{event: backtestEvent("same", at, usage.EventConsume, 1)}).Run(context.Background(), time.Time{}, time.Time{})
	if err == nil || err.Error() != "backtest source did not advance cursor" {
		t.Fatalf("error=%v", err)
	}
}
