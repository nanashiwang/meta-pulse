package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type overviewMemory struct {
	periods   []ports.PeriodOverview
	rules     map[uint64][]ports.EconomicsRuleOverview
	cursors   []ports.CursorOverview
	ruleCalls []uint64
	failRules bool
}

func (m *overviewMemory) ListPeriods(_ context.Context, limit int) ([]ports.PeriodOverview, error) {
	if limit > 0 && limit < len(m.periods) {
		return m.periods[:limit], nil
	}
	return m.periods, nil
}

func (m *overviewMemory) ListRules(_ context.Context, periodID uint64) ([]ports.EconomicsRuleOverview, error) {
	m.ruleCalls = append(m.ruleCalls, periodID)
	if m.failRules {
		return nil, errors.New("simulated rule read failure")
	}
	return m.rules[periodID], nil
}

func (m *overviewMemory) ListCursors(_ context.Context, _ time.Time) ([]ports.CursorOverview, error) {
	return m.cursors, nil
}

type operationsSnapshotStub struct {
	snapshot ports.OperationalSnapshot
	err      error
}

func (s operationsSnapshotStub) Snapshot(_ context.Context, _ time.Time, _, _ string) (ports.OperationalSnapshot, error) {
	return s.snapshot, s.err
}

type overviewUnit struct {
	overview   *overviewMemory
	operations ports.OperationsRepository
}

func (u *overviewUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Overview: u.overview, Operations: u.operations})
}

var overviewNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func activePeriodOverview() ports.PeriodOverview {
	return ports.PeriodOverview{
		ID: 1, Key: "2026-P001", Status: "active",
		StartsAt:  overviewNow.Add(-4 * 24 * time.Hour),
		EndsAt:    overviewNow.Add(6 * 24 * time.Hour),
		RuleCount: 1, UserCount: 12, UsageEventCount: 900, EntitledTickets: 30, SpentTickets: 4,
	}
}

func newOverviewService(t *testing.T, memory *overviewMemory, operations ports.OperationsRepository) *OperationsOverviewService {
	t.Helper()
	svc, err := NewOperationsOverviewService(&overviewUnit{overview: memory, operations: operations},
		OperationsOverviewConfig{}, func() time.Time { return overviewNow })
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestLoadReportsHealthyWhenAnActivePeriodHasRules(t *testing.T) {
	memory := &overviewMemory{
		periods: []ports.PeriodOverview{activePeriodOverview()},
		rules:   map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default", Eligible: true, MultiplierBps: 10000}}},
	}
	result, err := newOverviewService(t, memory, nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Health.HasActivePeriod || result.Health.IngestBlocked {
		t.Fatalf("health = %+v, want an unblocked active period", result.Health)
	}
	if len(result.Periods) != 1 || len(result.Periods[0].Rules) != 1 {
		t.Fatalf("periods = %+v", result.Periods)
	}
	if result.Periods[0].EntitledTickets != 30 {
		t.Fatalf("entitled tickets = %d, want 30", result.Periods[0].EntitledTickets)
	}
}

// This is the exact production condition that stalled ingestion: rows are
// fetched every batch but no period can accept them.
func TestLoadReportsBlockedWhenNoActivePeriodExists(t *testing.T) {
	memory := &overviewMemory{periods: []ports.PeriodOverview{{ID: 9, Key: "2026-P000", Status: "closed"}}}
	result, err := newOverviewService(t, memory, nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Health.HasActivePeriod || !result.Health.IngestBlocked {
		t.Fatalf("health = %+v, want blocked with no active period", result.Health)
	}
	if !strings.Contains(result.Health.BlockedReason, "does not advance the cursor") {
		t.Fatalf("blocked reason = %q", result.Health.BlockedReason)
	}
}

// An active period whose window has already passed cannot accept events even
// though its status column still says active.
func TestLoadTreatsAnExpiredActiveWindowAsBlocked(t *testing.T) {
	expired := activePeriodOverview()
	expired.StartsAt = overviewNow.Add(-20 * 24 * time.Hour)
	expired.EndsAt = overviewNow.Add(-10 * 24 * time.Hour)
	memory := &overviewMemory{periods: []ports.PeriodOverview{expired}, rules: map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default"}}}}
	result, err := newOverviewService(t, memory, nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Health.IngestBlocked {
		t.Fatal("an expired active window was reported as healthy")
	}
}

// A rule-less active period silently records every event as ineligible with
// zero contribution, which is worse than a hard failure because it looks fine.
func TestLoadFlagsAnActivePeriodWithNoRules(t *testing.T) {
	ruleless := activePeriodOverview()
	ruleless.RuleCount = 0
	memory := &overviewMemory{periods: []ports.PeriodOverview{ruleless}, rules: map[uint64][]ports.EconomicsRuleOverview{}}
	result, err := newOverviewService(t, memory, nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Health.HasActivePeriod || !result.Health.IngestBlocked {
		t.Fatalf("health = %+v, want an active but blocked period", result.Health)
	}
	if !strings.Contains(result.Health.BlockedReason, "ineligible") {
		t.Fatalf("blocked reason = %q", result.Health.BlockedReason)
	}
}

// Closed periods are history. Fetching their rules on every console refresh
// would cost a query per period for data that cannot change.
func TestLoadFetchesRulesOnlyForDraftAndActivePeriods(t *testing.T) {
	memory := &overviewMemory{
		periods: []ports.PeriodOverview{
			activePeriodOverview(),
			{ID: 2, Key: "2026-P002", Status: "draft"},
			{ID: 3, Key: "2026-P000", Status: "closed"},
			{ID: 4, Key: "2026-P00X", Status: "settling"},
		},
		rules: map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default"}}},
	}
	if _, err := newOverviewService(t, memory, nil).Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(memory.ruleCalls) != 2 {
		t.Fatalf("rule queries = %v, want only the draft and active periods", memory.ruleCalls)
	}
}

func TestLoadIncludesCursorProgress(t *testing.T) {
	watermark := overviewNow.Add(-90 * time.Second)
	memory := &overviewMemory{
		periods: []ports.PeriodOverview{activePeriodOverview()},
		rules:   map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default"}}},
		cursors: []ports.CursorOverview{{Name: "new-api-usage", Value: "1789487999:42", WatermarkAt: &watermark, LagSeconds: 90}},
	}
	result, err := newOverviewService(t, memory, nil).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Cursors) != 1 || result.Cursors[0].LagSeconds != 90 {
		t.Fatalf("cursors = %+v", result.Cursors)
	}
}

// The snapshot is a diagnostic projection. Losing it must not blank the page
// that tells the operator why ingestion is stalled.
func TestLoadStillRendersWhenTheSnapshotFails(t *testing.T) {
	memory := &overviewMemory{periods: []ports.PeriodOverview{activePeriodOverview()}, rules: map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default"}}}}
	result, err := newOverviewService(t, memory, operationsSnapshotStub{err: errors.New("metrics unavailable")}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot != nil {
		t.Fatal("a failed snapshot was rendered as data")
	}
	if len(result.Periods) != 1 {
		t.Fatal("period list was dropped along with the snapshot")
	}
}

func TestLoadIncludesSnapshotWhenAvailable(t *testing.T) {
	memory := &overviewMemory{periods: []ports.PeriodOverview{activePeriodOverview()}, rules: map[uint64][]ports.EconomicsRuleOverview{1: {{RuleKey: "default"}}}}
	stub := operationsSnapshotStub{snapshot: ports.OperationalSnapshot{IngestLagSeconds: 12, SettlementDeadCount: 3}}
	result, err := newOverviewService(t, memory, stub).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot == nil || result.Snapshot.SettlementDeadCount != 3 {
		t.Fatalf("snapshot = %+v", result.Snapshot)
	}
}

// A failed rule read must fail the request rather than render a period as
// having no rules, which the console reports as a blocking fault.
func TestLoadFailsWhenRulesCannotBeRead(t *testing.T) {
	memory := &overviewMemory{periods: []ports.PeriodOverview{activePeriodOverview()}, failRules: true}
	if _, err := newOverviewService(t, memory, nil).Load(context.Background()); err == nil {
		t.Fatal("a failed rule read was reported as success")
	}
}
