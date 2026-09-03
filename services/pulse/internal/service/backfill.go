package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type BackfillOptions struct {
	From   time.Time
	To     time.Time
	DryRun bool
}

type BackfillReport struct {
	DryRun        bool `json:"dry_run"`
	Fetched       int  `json:"fetched"`
	InRange       int  `json:"in_range"`
	WouldReview   int  `json:"would_review"`
	Accepted      int  `json:"accepted"`
	Replayed      int  `json:"replayed"`
	Conflicts     int  `json:"conflicts"`
	SkippedBefore int  `json:"skipped_before"`
}

// BackfillService uses the exact normalized events and processOne path as
// realtime ingest, but keeps an independent cursor so a historical replay
// cannot move the realtime watermark.
type BackfillService struct {
	ingest *UsageIngestService
	source ports.UsageSource
}

func NewBackfillService(ingest *UsageIngestService, source ports.UsageSource) (*BackfillService, error) {
	if ingest == nil || source == nil {
		return nil, errors.New("backfill dependencies are nil")
	}
	return &BackfillService{ingest: ingest, source: source}, nil
}

func (s *BackfillService) Run(ctx context.Context, options BackfillOptions) (BackfillReport, error) {
	if !options.From.IsZero() && !options.To.IsZero() && options.To.Before(options.From) {
		return BackfillReport{}, errors.New("backfill to time is before from time")
	}
	report := BackfillReport{DryRun: options.DryRun}
	var after string
	if !options.DryRun {
		var err error
		after, err = s.cursor(ctx)
		if err != nil {
			return report, err
		}
	}
	for {
		pageStart := after
		events, err := s.source.Fetch(ctx, after, s.ingest.batchSize)
		if err != nil {
			return report, err
		}
		if len(events) == 0 {
			return report, nil
		}
		for _, event := range events {
			report.Fetched++
			if !options.To.IsZero() && event.SourceCreatedAt.After(options.To) {
				return report, nil
			}
			if !options.From.IsZero() && event.SourceCreatedAt.Before(options.From) {
				report.SkippedBefore++
				if !options.DryRun {
					if err := s.ingest.advanceOnly(ctx, event); err != nil {
						return report, err
					}
				}
				after = event.CursorValue
				continue
			}
			report.InRange++
			if event.NeedsReview {
				report.WouldReview++
			}
			if options.DryRun {
				after = event.CursorValue
				continue
			}
			before := IngestResult{}
			if err := s.ingest.processOne(ctx, event, &before); err != nil {
				return report, fmt.Errorf("backfill event %s: %w", event.SourceEventID, err)
			}
			report.Accepted += before.Accepted
			report.Replayed += before.Replayed
			report.Conflicts += before.Conflicts
			after = event.CursorValue
		}
		if after == pageStart {
			return report, errors.New("backfill source did not advance cursor")
		}
	}
}

func (s *BackfillService) cursor(ctx context.Context) (string, error) {
	var value string
	err := s.ingest.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Cursor == nil {
			return errors.New("cursor repository is not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.ingest.cursorName, s.ingest.sourceSystem)
		if err != nil {
			return err
		}
		value = cursor.Value
		return nil
	})
	return value, err
}

func (s *UsageIngestService) advanceOnly(ctx context.Context, event usage.Event) error {
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Cursor == nil {
			return errors.New("cursor repository is not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cursorName, s.sourceSystem)
		if err != nil {
			return err
		}
		return saveCursor(ctx, repos.Cursor, cursor, event)
	})
}
