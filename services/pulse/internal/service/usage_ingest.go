package service

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const DefaultUsageCursorName = "new-api-usage"

type UsageIngestService struct {
	unit                 ports.UnitOfWork
	source               ports.UsageSource
	cursorName           string
	sourceSystem         string
	batchSize            int
	ticketThresholdMilli int64
}

type UsageIngestConfig struct {
	CursorName           string
	SourceSystem         string
	BatchSize            int
	TicketThresholdMilli int64
}

type IngestResult struct {
	Fetched         int `json:"fetched"`
	Accepted        int `json:"accepted"`
	ManualReview    int `json:"manual_review"`
	Replayed        int `json:"replayed"`
	Conflicts       int `json:"conflicts"`
	TicketsMinted   int `json:"tickets_minted"`
	TicketsReversed int `json:"tickets_reversed"`
}

func NewUsageIngestService(unit ports.UnitOfWork, source ports.UsageSource, cfg UsageIngestConfig) (*UsageIngestService, error) {
	if unit == nil || source == nil {
		return nil, errors.New("usage ingest dependencies are nil")
	}
	if cfg.CursorName == "" {
		cfg.CursorName = DefaultUsageCursorName
	}
	if cfg.SourceSystem == "" {
		cfg.SourceSystem = "new-api-log"
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 5000 {
		return nil, errors.New("usage ingest batch size must be between 1 and 5000")
	}
	if cfg.TicketThresholdMilli <= 0 {
		return nil, errors.New("ticket threshold must be positive")
	}
	return &UsageIngestService{unit: unit, source: source, cursorName: cfg.CursorName, sourceSystem: cfg.SourceSystem, batchSize: cfg.BatchSize, ticketThresholdMilli: cfg.TicketThresholdMilli}, nil
}

// IngestBatch advances the durable cursor only in the same transaction as the
// event and all accounting effects. A crash before commit therefore replays
// safely; a crash after commit resumes after the last committed event.
func (s *UsageIngestService) IngestBatch(ctx context.Context) (IngestResult, error) {
	var result IngestResult
	var cursorValue string
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Cursor == nil {
			return errors.New("cursor repository is not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cursorName, s.sourceSystem)
		if err != nil {
			return err
		}
		cursorValue = cursor.Value
		return nil
	})
	if err != nil {
		return result, err
	}
	events, err := s.source.Fetch(ctx, cursorValue, s.batchSize)
	if err != nil {
		return result, err
	}
	result.Fetched = len(events)
	for _, event := range events {
		if err := s.processOne(ctx, event, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (s *UsageIngestService) processOne(ctx context.Context, incoming usage.Event, result *IngestResult) error {
	if incoming.SourceSystem == "" {
		incoming.SourceSystem = s.sourceSystem
	}
	if incoming.SourceSystem != s.sourceSystem || incoming.SourceEventID == "" || incoming.CursorValue == "" || incoming.PayloadHash == "" || incoming.SourceCreatedAt.IsZero() {
		return errors.New("invalid normalized usage event")
	}
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Usage == nil || repos.Conflict == nil || repos.Cursor == nil || repos.Period == nil || repos.Economics == nil || repos.UserPeriod == nil {
			return errors.New("usage ingest repositories are not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cursorName, s.sourceSystem)
		if err != nil {
			return err
		}
		existing, err := repos.Usage.FindBySource(ctx, incoming.SourceSystem, incoming.SourceEventID)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		if existing != nil {
			if existing.PayloadHash != incoming.PayloadHash {
				if err := repos.Conflict.Create(ctx, ports.IngestConflict{SourceSystem: incoming.SourceSystem, SourceEventID: incoming.SourceEventID, ExistingPayloadHash: existing.PayloadHash, IncomingPayloadHash: incoming.PayloadHash, Reason: "same source event has different payload"}); err != nil {
					return err
				}
				result.Conflicts++
			} else {
				result.Replayed++
			}
			return saveCursor(ctx, repos.Cursor, cursor, incoming)
		}

		activity, err := repos.Period.FindActiveAt(ctx, incoming.SourceCreatedAt)
		if err != nil {
			// Do not advance the cursor while no period is active. Operations can
			// activate/fix the period and safely retry the same source row.
			return err
		}
		incoming.PeriodID = activity.ID

		if incoming.NeedsReview {
			incoming.Status = usage.StatusManualReview
			incoming.ReviewReason = nonEmptyReason(incoming.ReviewReason, "source mapper requested manual review")
			incoming.Eligible = false
			incoming.ContributionMilli = 0
			incoming.MultiplierBps = 0
			if _, err := repos.Usage.Create(ctx, incoming); err != nil {
				return err
			}
			if err := recordReviewConflict(ctx, repos.Conflict, incoming); err != nil {
				return err
			}
			result.ManualReview++
			return saveCursor(ctx, repos.Cursor, cursor, incoming)
		}

		rules, err := repos.Economics.ListRules(ctx, activity.ID)
		if err != nil {
			return err
		}
		rule, found := economics.Select(rules, incoming.ModelName, incoming.ChannelID)
		if found {
			decision, err := economics.Evaluate(incoming.QuotaDelta, rule)
			if err != nil {
				return err
			}
			incoming.Eligible = decision.Eligible
			incoming.EconomicsRuleID = uint64Ptr(decision.RuleID)
			incoming.MultiplierBps = decision.MultiplierBps
			incoming.ContributionMilli = decision.Contribution
		} else {
			// An absent rule is a recorded, ineligible event—not an implicit
			// default reward rule. This makes rule gaps visible in backtests.
			incoming.Eligible = false
			incoming.MultiplierBps = 0
			incoming.ContributionMilli = 0
		}
		incoming.Status = usage.StatusAccepted

		var contributionEntry ledger.Entry
		if incoming.Eligible && incoming.ContributionMilli != 0 {
			if incoming.EventType == usage.EventRefund {
				original, err := s.resolveRefund(ctx, repos, incoming)
				if err != nil {
					incoming.Status = usage.StatusManualReview
					incoming.ReviewReason = err.Error()
					incoming.Eligible = false
					incoming.ContributionMilli = 0
					incoming.MultiplierBps = 0
					if _, createErr := repos.Usage.Create(ctx, incoming); createErr != nil {
						return createErr
					}
					if reviewErr := recordReviewConflict(ctx, repos.Conflict, incoming); reviewErr != nil {
						return reviewErr
					}
					result.ManualReview++
					return saveCursor(ctx, repos.Cursor, cursor, incoming)
				}
				contributionEntry, err = s.appendContribution(ctx, repos, incoming, &original.ID)
			} else {
				contributionEntry, err = s.appendContribution(ctx, repos, incoming, nil)
			}
			if err != nil {
				return err
			}
		}

		stat, err := repos.UserPeriod.GetOrCreateForUpdate(ctx, incoming.UserID, incoming.PeriodID)
		if err != nil {
			return err
		}
		newNet := stat.NetContributionMilli
		if incoming.Eligible && incoming.ContributionMilli != 0 {
			newNet = contributionEntry.BalanceAfter
		}
		oldEntitled := entitledTickets(stat.NetContributionMilli, s.ticketThresholdMilli)
		newEntitled := entitledTickets(newNet, s.ticketThresholdMilli)
		ticketDelta := newEntitled - oldEntitled
		if ticketDelta != 0 {
			operation := ledger.OperationTicketMint
			var reversalOf *uint64
			if ticketDelta < 0 {
				operation = ledger.OperationTicketReverse
				// The contribution reversal is the causal event. The linkage is
				// retained even when several mint entries are being clawed back.
				reversalOf = &contributionEntry.ID
			}
			ticketEntry := ledger.Entry{UserID: incoming.UserID, PeriodID: incoming.PeriodID, AssetType: ledger.AssetTicket, Operation: operation, Amount: ticketDelta, SourceType: "usage", SourceRef: usageSourceRef(incoming), IdempotencyKey: "ticket:" + incoming.SourceSystem + ":" + incoming.SourceEventID, PayloadHash: incoming.PayloadHash, ReversalOfEntryID: reversalOf, Reason: "usage entitlement delta"}
			if _, err := appendEntry(ctx, repos, ticketEntry); err != nil {
				return err
			}
			if ticketDelta > 0 {
				result.TicketsMinted += int(ticketDelta)
			} else {
				result.TicketsReversed += int(-ticketDelta)
			}
		}
		stat.NetContributionMilli = newNet
		stat.EntitledTickets = newEntitled
		if stat.UsageEventCount == math.MaxUint64 {
			return errors.New("usage event count overflow")
		}
		stat.UsageEventCount++
		stat.Version++
		if err := repos.UserPeriod.Save(ctx, stat); err != nil {
			return err
		}
		if _, err := repos.Usage.Create(ctx, incoming); err != nil {
			return err
		}
		result.Accepted++
		return saveCursor(ctx, repos.Cursor, cursor, incoming)
	})
}

func (s *UsageIngestService) appendContribution(ctx context.Context, repos ports.Repositories, event usage.Event, reversalOf *uint64) (ledger.Entry, error) {
	operation := ledger.OperationContributionEarn
	if event.EventType == usage.EventRefund || event.EventType == usage.EventCorrect {
		operation = ledger.OperationContributionReverse
	}
	return appendEntry(ctx, repos, ledger.Entry{UserID: event.UserID, PeriodID: event.PeriodID, AssetType: ledger.AssetContribution, Operation: operation, Amount: int64(event.ContributionMilli), SourceType: "usage", SourceRef: usageSourceRef(event), IdempotencyKey: "contribution:" + event.SourceSystem + ":" + event.SourceEventID, PayloadHash: event.PayloadHash, ReversalOfEntryID: reversalOf})
}

func (s *UsageIngestService) resolveRefund(ctx context.Context, repos ports.Repositories, event usage.Event) (ledger.Entry, error) {
	if event.RelatedSourceEventID == "" && event.RequestID != "" {
		original, err := repos.Usage.FindConsumeByRequest(ctx, event.UserID, event.RequestID)
		if err != nil {
			return ledger.Entry{}, fmt.Errorf("refund consume correlation unavailable: %w", err)
		}
		event.RelatedSourceEventID = original.SourceEventID
	}
	if event.RelatedSourceEventID == "" {
		return ledger.Entry{}, errors.New("refund has no stable consume correlation")
	}
	original, err := repos.Ledger.FindBySource(ctx, "usage", usageSourceRefForID(event.SourceSystem, event.RelatedSourceEventID), ledger.AssetContribution)
	if err != nil {
		return ledger.Entry{}, fmt.Errorf("refund original ledger unavailable: %w", err)
	}
	if original.UserID != event.UserID || original.PeriodID != event.PeriodID {
		return ledger.Entry{}, errors.New("refund consume correlation belongs to another account")
	}
	return *original, nil
}

func saveCursor(ctx context.Context, repository ports.CursorRepository, cursor ports.Cursor, event usage.Event) error {
	cursor.Value = event.CursorValue
	watermark := event.SourceCreatedAt
	cursor.WatermarkAt = &watermark
	if cursor.Version == math.MaxUint64 {
		return errors.New("worker cursor version overflow")
	}
	cursor.Version++
	return repository.Save(ctx, cursor)
}

func recordReviewConflict(ctx context.Context, repository ports.ConflictRepository, event usage.Event) error {
	return repository.Create(ctx, ports.IngestConflict{SourceSystem: event.SourceSystem, SourceEventID: event.SourceEventID, IncomingPayloadHash: event.PayloadHash, Reason: nonEmptyReason(event.ReviewReason, "manual review required")})
}

func usageSourceRef(event usage.Event) string {
	return usageSourceRefForID(event.SourceSystem, event.SourceEventID)
}

func usageSourceRefForID(sourceSystem, eventID string) string {
	return sourceSystem + ":" + eventID
}

func entitledTickets(netContribution, threshold int64) int64 {
	if netContribution <= 0 {
		return 0
	}
	return netContribution / threshold
}

func uint64Ptr(value uint64) *uint64 {
	if value == 0 {
		return nil
	}
	return &value
}

func nonEmptyReason(reason, fallback string) string {
	if reason != "" {
		return reason
	}
	return fallback
}
