package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// A cursor-aware source detects skipped tails rather than blindly replaying
// the same fixture page after a yield.
type resumableUsageSource struct {
	events []usage.Event
	err    error
}

func (s resumableUsageSource) Fetch(_ context.Context, cursor string, limit int) ([]usage.Event, error) {
	if s.err != nil {
		return nil, s.err
	}
	start := 0
	if cursor != "" {
		start = -1
		for i, event := range s.events {
			if event.CursorValue == cursor {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return nil, errors.New("unexpected cursor")
		}
	}
	end := start + limit
	if end > len(s.events) {
		end = len(s.events)
	}
	return s.events[start:end], nil
}

func budgetIngestFixture(t *testing.T) (*UsageIngestService, *memoryLedgerStore) {
	t.Helper()
	store := newMemoryLedgerStore()
	store.periods = []period.Period{{ID: 4, Status: period.StatusActive, ConfigVersion: "v1", StartsAt: time.Unix(1_600_000_000, 0), EndsAt: time.Unix(1_800_000_000, 0)}}
	store.rules[4] = []economics.Rule{{ID: 8, Key: "default", Eligible: true, MultiplierBps: 10000, ConfigVersion: "v1"}}
	svc := newIngestService(t, store, resumableUsageSource{events: []usage.Event{
		ingestEvent("1", "hash-1", usage.EventConsume, 1000),
		ingestEvent("2", "hash-2", usage.EventConsume, 1000),
		ingestEvent("3", "hash-3", usage.EventConsume, 1000),
	}})
	svc.batchTimeBudget = 15 * time.Second
	clock := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { now := clock; clock = clock.Add(15 * time.Second); return now }
	return svc, store
}

func TestUsageBudgetYieldsOnlyCommittedEventsAndResumesTail(t *testing.T) {
	svc, store := budgetIngestFixture(t)
	for i := 0; i < 3; i++ {
		result, err := svc.IngestBatch(context.Background())
		if err != nil || result.Accepted != 1 || result.Fetched != 3-i || result.Yielded != (i < 2) {
			t.Fatalf("batch %d: result=%+v err=%v", i, result, err)
		}
		if len(store.usageEvents) != i+1 || store.cursor.Value != svc.source.(resumableUsageSource).events[i].CursorValue {
			t.Fatalf("batch %d skipped or duplicated events: cursor=%+v", i, store.cursor)
		}
	}
	result, err := svc.IngestBatch(context.Background())
	if err != nil || result.Fetched != 0 || result.Yielded {
		t.Fatalf("empty batch: %+v %v", result, err)
	}
	if len(store.entries) != 6 || store.accounts[accountKey(9, 4, "contribution")].Balance != 3000 || store.accounts[accountKey(9, 4, "ticket")].Balance != 3 {
		t.Fatal("yield/resume changed accounting")
	}
}

func TestUsageBudgetZeroProcessesFullPage(t *testing.T) {
	svc, _ := budgetIngestFixture(t)
	svc.batchTimeBudget = 0
	result, err := svc.IngestBatch(context.Background())
	if err != nil || result.Yielded || result.Accepted != 3 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestUsageBudgetDoesNotHideDependencyOrPeriodFailures(t *testing.T) {
	for _, kind := range []string{"source", "period", "canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			svc, store := budgetIngestFixture(t)
			ctx := context.Background()
			switch kind {
			case "source":
				svc.source = resumableUsageSource{err: errors.New("source unavailable")}
			case "period":
				store.periods = nil
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "deadline":
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				ctx = expired
			}
			result, err := svc.IngestBatch(ctx)
			if err == nil || result.Yielded || result.Accepted != 0 || len(store.entries) != 0 || store.cursor.Value != "" {
				t.Fatalf("failure became progress: result=%+v err=%v cursor=%+v", result, err, store.cursor)
			}
		})
	}
}

type failedCommitUnit struct {
	inner ports.UnitOfWork
	calls int
}

func (u *failedCommitUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	u.calls++
	if err := u.inner.Do(ctx, fn); err != nil {
		return err
	}
	if u.calls == 2 {
		return errors.New("commit failed")
	}
	return nil
}

func TestUsageBudgetDoesNotReportFailedCommitAsProgress(t *testing.T) {
	svc, _ := budgetIngestFixture(t)
	svc.unit = &failedCommitUnit{inner: svc.unit}
	result, err := svc.IngestBatch(context.Background())
	if err == nil || result.Yielded || result.Accepted != 0 || result.TicketsMinted != 0 {
		t.Fatalf("failed commit counted: result=%+v err=%v", result, err)
	}
}
