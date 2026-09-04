package service

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type staticUsageSource struct{ events []usage.Event }

func (s staticUsageSource) Fetch(_ context.Context, _ string, _ int) ([]usage.Event, error) {
	return append([]usage.Event(nil), s.events...), nil
}

func newIngestService(t *testing.T, store *memoryLedgerStore, source ports.UsageSource) *UsageIngestService {
	t.Helper()
	service, err := NewUsageIngestService(memoryUnit{store: store}, source, UsageIngestConfig{BatchSize: 100, TicketThresholdMilli: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func ingestEvent(id, hash string, eventType usage.EventType, quota int64) usage.Event {
	return usage.Event{SourceSystem: "new-api-log", SourceEventID: id, CursorValue: "1:" + id, PayloadHash: hash, UserID: 9, EventType: eventType, SourceCreatedAt: time.Unix(1_700_000_000, 0).UTC(), QuotaDelta: quota, ModelName: "gpt-4o", ChannelID: 2}
}

func TestUsageIngestReplayAndConflictAreSafe(t *testing.T) {
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0)}}
	store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000}}
	event := ingestEvent("1", "hash-1", usage.EventConsume, 1500)
	s := newIngestService(t, store, staticUsageSource{events: []usage.Event{event}})

	var first IngestResult
	if err := s.processOne(context.Background(), event, &first); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		if err := s.processOne(context.Background(), event, &first); err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	if len(store.usageEvents) != 1 || len(store.entries) != 2 || first.Replayed != 99 || first.Accepted != 1 {
		t.Fatalf("result=%+v usage=%d ledger=%d", first, len(store.usageEvents), len(store.entries))
	}
	if got := store.accounts[accountKey(9, 4, "contribution")].Balance; got != 1500 {
		t.Fatalf("contribution account = %d", got)
	}
	if got := store.accounts[accountKey(9, 4, "ticket")].Balance; got != 1 {
		t.Fatalf("ticket account = %d, want 1", got)
	}

	changed := event
	changed.PayloadHash = "hash-2"
	if err := s.processOne(context.Background(), changed, &first); err != nil {
		t.Fatal(err)
	}
	if first.Conflicts != 1 || len(store.conflicts) != 1 || len(store.entries) != 2 {
		t.Fatalf("conflict result=%+v conflicts=%d ledger=%d", first, len(store.conflicts), len(store.entries))
	}
}

func TestUsageIngestRefundReversesContributionAndTicketEntitlement(t *testing.T) {
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0)}}
	store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000}}
	consume := ingestEvent("1", "hash-1", usage.EventConsume, 1500)
	s := newIngestService(t, store, staticUsageSource{})
	result := IngestResult{}
	if err := s.processOne(context.Background(), consume, &result); err != nil {
		t.Fatal(err)
	}
	refund := ingestEvent("2", "hash-2", usage.EventRefund, -1000)
	refund.RelatedSourceEventID = "1"
	if err := s.processOne(context.Background(), refund, &result); err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 2 || result.TicketsReversed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if got := store.accounts[accountKey(9, 4, "contribution")].Balance; got != 500 {
		t.Fatalf("contribution account = %d, want 500", got)
	}
	if got := store.accounts[accountKey(9, 4, "ticket")].Balance; got != 0 {
		t.Fatalf("ticket account = %d, want 0", got)
	}
}

func TestUsageIngestUncorrelatedRefundGoesToReview(t *testing.T) {
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0)}}
	store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000}}
	refund := ingestEvent("2", "hash-2", usage.EventRefund, -1000)
	refund.QuotaDelta = 500 // mapper would normalize this before the service.
	refund.NeedsReview = true
	refund.ReviewReason = "refund has no stable consume correlation"
	s := newIngestService(t, store, staticUsageSource{})
	var result IngestResult
	if err := s.processOne(context.Background(), refund, &result); err != nil {
		t.Fatal(err)
	}
	if result.ManualReview != 1 || len(store.entries) != 0 || store.usageEvents[0].Status != usage.StatusManualReview {
		t.Fatalf("result=%+v usage=%+v ledger=%d", result, store.usageEvents, len(store.entries))
	}
}

type cursorAwareSource struct{ events []usage.Event }

func (s cursorAwareSource) Fetch(_ context.Context, after string, _ int) ([]usage.Event, error) {
	if after != "" {
		return nil, nil
	}
	return append([]usage.Event(nil), s.events...), nil
}

func TestBackfillDryRunDoesNotWritePulseState(t *testing.T) {
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0)}}
	event := ingestEvent("1", "hash-1", usage.EventConsume, 100)
	ingest := newIngestService(t, store, cursorAwareSource{events: []usage.Event{event}})
	backfill, err := NewBackfillService(ingest, cursorAwareSource{events: []usage.Event{event}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := backfill.Run(context.Background(), BackfillOptions{DryRun: true, From: event.SourceCreatedAt.Add(-time.Minute), To: event.SourceCreatedAt.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if report.Fetched != 1 || report.InRange != 1 || len(store.usageEvents) != 0 || len(store.entries) != 0 || store.cursor.Value != "" {
		t.Fatalf("report=%+v usage=%d ledger=%d cursor=%+v", report, len(store.usageEvents), len(store.entries), store.cursor)
	}
}

func TestBackfillUsesHalfOpenEnd(t *testing.T) {
	store := newMemoryLedgerStore()
	start := time.Unix(1_700_000_000, 0).UTC()
	boundary := start.Add(time.Hour)
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, StartsAt: start.Add(-time.Hour), EndsAt: boundary.Add(time.Hour)}}
	store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000}}

	inRange := ingestEvent("1", "hash-1", usage.EventConsume, 100)
	inRange.SourceCreatedAt = start.Add(30 * time.Minute)
	atBoundary := ingestEvent("2", "hash-2", usage.EventConsume, 200)
	atBoundary.SourceCreatedAt = boundary
	backfill, err := NewBackfillService(
		newIngestService(t, store, cursorAwareSource{events: []usage.Event{inRange, atBoundary}}),
		cursorAwareSource{events: []usage.Event{inRange, atBoundary}},
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := backfill.Run(context.Background(), BackfillOptions{From: start, To: boundary})
	if err != nil {
		t.Fatal(err)
	}
	if report.Fetched != 2 || report.InRange != 1 || report.Accepted != 1 {
		t.Fatalf("report=%+v, want only the event before the end boundary", report)
	}
	if len(store.usageEvents) != 1 || store.usageEvents[0].SourceEventID != "1" {
		t.Fatalf("usage events=%+v, want only event 1", store.usageEvents)
	}
}
