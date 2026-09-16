package service

import (
	"context"
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// OperationsOverview is everything the operations console renders in one
// round trip: which periods exist, what rules a period froze, and how far
// ingestion has progressed.
type OperationsOverview struct {
	ObservedAt time.Time                 `json:"observed_at"`
	Periods    []PeriodOverviewItem      `json:"periods"`
	Cursors    []CursorOverviewItem      `json:"cursors"`
	Health     OperationsOverviewHealth  `json:"health"`
	Snapshot   *OperationsOverviewCounts `json:"snapshot,omitempty"`
}

type PeriodOverviewItem struct {
	ID              uint64              `json:"id"`
	Key             string              `json:"key"`
	Status          string              `json:"status"`
	StartsAt        time.Time           `json:"starts_at"`
	EndsAt          time.Time           `json:"ends_at"`
	Timezone        string              `json:"timezone"`
	ConfigVersion   string              `json:"config_version"`
	RandomVersion   string              `json:"random_version"`
	RuleCount       int64               `json:"rule_count"`
	UserCount       int64               `json:"user_count"`
	UsageEventCount int64               `json:"usage_event_count"`
	EntitledTickets int64               `json:"entitled_tickets"`
	SpentTickets    int64               `json:"spent_tickets"`
	Rules           []EconomicsRuleItem `json:"rules,omitempty"`
}

type EconomicsRuleItem struct {
	RuleKey       string  `json:"rule_key"`
	Priority      int     `json:"priority"`
	ModelPattern  string  `json:"model_pattern,omitempty"`
	ChannelID     *uint64 `json:"channel_id,omitempty"`
	Eligible      bool    `json:"eligible"`
	MultiplierBps int32   `json:"multiplier_bps"`
	ConfigVersion string  `json:"config_version"`
}

type CursorOverviewItem struct {
	Name         string     `json:"name"`
	SourceSystem string     `json:"source_system"`
	Value        string     `json:"value"`
	WatermarkAt  *time.Time `json:"watermark_at"`
	Version      uint64     `json:"version"`
	LagSeconds   int64      `json:"lag_seconds"`
}

// OperationsOverviewHealth answers the one question the console exists to
// answer at a glance: can usage ingestion currently record anything?
//
// IngestBlocked is not a guess about the worker. With no active period,
// usage_ingest fails every batch by construction and deliberately does not
// advance the cursor, so the condition is a property of the data.
type OperationsOverviewHealth struct {
	HasActivePeriod bool   `json:"has_active_period"`
	IngestBlocked   bool   `json:"ingest_blocked"`
	BlockedReason   string `json:"blocked_reason,omitempty"`
}

type OperationsOverviewCounts struct {
	IngestLagSeconds     int64 `json:"ingest_lag_seconds"`
	OpenConflictCount    int64 `json:"open_conflict_count"`
	LedgerMismatchCount  int64 `json:"ledger_mismatch_count"`
	SettlementRetryCount int64 `json:"settlement_retry_count"`
	SettlementDeadCount  int64 `json:"settlement_dead_count"`
}

type OperationsOverviewConfig struct {
	CursorName   string
	SourceSystem string
	PeriodLimit  int
}

type OperationsOverviewService struct {
	unit ports.UnitOfWork
	cfg  OperationsOverviewConfig
	now  func() time.Time
}

func NewOperationsOverviewService(unit ports.UnitOfWork, cfg OperationsOverviewConfig, now func() time.Time) (*OperationsOverviewService, error) {
	if unit == nil {
		return nil, errors.New("operations overview unit of work is nil")
	}
	if cfg.CursorName == "" {
		cfg.CursorName = DefaultUsageCursorName
	}
	if cfg.SourceSystem == "" {
		cfg.SourceSystem = "new-api-log"
	}
	if cfg.PeriodLimit <= 0 {
		cfg.PeriodLimit = 20
	}
	if now == nil {
		now = time.Now
	}
	return &OperationsOverviewService{unit: unit, cfg: cfg, now: now}, nil
}

// Load reads the console projection. It never writes, so a console request
// cannot advance a cursor, mutate a period or touch the Ledger.
func (s *OperationsOverviewService) Load(ctx context.Context) (OperationsOverview, error) {
	observedAt := s.now()
	overview := OperationsOverview{ObservedAt: observedAt}

	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Overview == nil {
			return errors.New("operations overview repository is not initialized")
		}
		periods, err := repos.Overview.ListPeriods(ctx, s.cfg.PeriodLimit)
		if err != nil {
			return err
		}
		cursors, err := repos.Overview.ListCursors(ctx, observedAt)
		if err != nil {
			return err
		}

		overview.Periods = make([]PeriodOverviewItem, 0, len(periods))
		for _, source := range periods {
			item := PeriodOverviewItem{
				ID: source.ID, Key: source.Key, Status: source.Status,
				StartsAt: source.StartsAt, EndsAt: source.EndsAt, Timezone: source.Timezone,
				ConfigVersion: source.ConfigVersion, RandomVersion: source.RandomVersion,
				RuleCount: source.RuleCount, UserCount: source.UserCount,
				UsageEventCount: source.UsageEventCount,
				EntitledTickets: source.EntitledTickets, SpentTickets: source.SpentTickets,
			}
			// Rules are fetched only for periods that can still affect ingest.
			// A closed period's rules are history; the console links to them
			// rather than paying for them on every refresh.
			if source.Status == "draft" || source.Status == "active" {
				rules, ruleErr := repos.Overview.ListRules(ctx, source.ID)
				if ruleErr != nil {
					return ruleErr
				}
				item.Rules = make([]EconomicsRuleItem, 0, len(rules))
				for _, rule := range rules {
					item.Rules = append(item.Rules, EconomicsRuleItem{
						RuleKey: rule.RuleKey, Priority: rule.Priority,
						ModelPattern: rule.ModelPattern, ChannelID: rule.ChannelID,
						Eligible: rule.Eligible, MultiplierBps: rule.MultiplierBps,
						ConfigVersion: rule.ConfigVersion,
					})
				}
			}
			overview.Periods = append(overview.Periods, item)
		}

		overview.Cursors = make([]CursorOverviewItem, 0, len(cursors))
		for _, source := range cursors {
			overview.Cursors = append(overview.Cursors, CursorOverviewItem{
				Name: source.Name, SourceSystem: source.SourceSystem, Value: source.Value,
				WatermarkAt: source.WatermarkAt, Version: source.Version, LagSeconds: source.LagSeconds,
			})
		}

		// The operational snapshot is a diagnostic projection and is allowed to
		// be unavailable; the rest of the console must still render.
		if repos.Operations != nil {
			if snapshot, snapErr := repos.Operations.Snapshot(ctx, observedAt, s.cfg.CursorName, s.cfg.SourceSystem); snapErr == nil {
				overview.Snapshot = &OperationsOverviewCounts{
					IngestLagSeconds:     snapshot.IngestLagSeconds,
					OpenConflictCount:    snapshot.OpenConflictCount,
					LedgerMismatchCount:  snapshot.LedgerMismatchCount,
					SettlementRetryCount: snapshot.SettlementRetryCount,
					SettlementDeadCount:  snapshot.SettlementDeadCount,
				}
			}
		}
		return nil
	})
	if err != nil {
		return OperationsOverview{}, err
	}

	overview.Health = deriveOverviewHealth(overview.Periods, observedAt)
	return overview, nil
}

// deriveOverviewHealth distinguishes the two ways ingestion stalls, because
// they need different operator actions: no active period at all (create one),
// versus an active period that froze no rules (every event lands ineligible
// with zero contribution, and invariant #11 means it cannot be repaired in
// place — the period must be closed and a new one created).
func deriveOverviewHealth(periods []PeriodOverviewItem, at time.Time) OperationsOverviewHealth {
	for _, item := range periods {
		if item.Status != "active" {
			continue
		}
		if at.Before(item.StartsAt) || !at.Before(item.EndsAt) {
			continue
		}
		if item.RuleCount == 0 {
			return OperationsOverviewHealth{
				HasActivePeriod: true, IngestBlocked: true,
				BlockedReason: "active period has no economics rule: every event records as ineligible with zero contribution",
			}
		}
		return OperationsOverviewHealth{HasActivePeriod: true}
	}
	return OperationsOverviewHealth{
		IngestBlocked: true,
		BlockedReason: "no active period covers the current time: usage ingest fails every batch and does not advance the cursor",
	}
}
