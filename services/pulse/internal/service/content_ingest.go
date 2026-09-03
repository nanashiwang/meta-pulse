package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const DefaultContentCursorName = "forum_content"
const DefaultContentSourceSystem = "answer-forum"

type ContentSource interface {
	Fetch(context.Context, string, int) ([]ports.ContentEvent, error)
}

type ContentIngestConfig struct {
	BatchSize    int
	CursorName   string
	SourceSystem string
	Now          func() time.Time
}

type ContentIngestResult struct {
	Fetched   int `json:"fetched"`
	Accepted  int `json:"accepted"`
	Replayed  int `json:"replayed"`
	Conflicts int `json:"conflicts"`
}

type ContentIngestService struct {
	unit   ports.UnitOfWork
	source ContentSource
	cfg    ContentIngestConfig
}

func NewContentIngestService(unit ports.UnitOfWork, source ContentSource, cfg ContentIngestConfig) (*ContentIngestService, error) {
	if unit == nil || source == nil {
		return nil, errors.New("content ingest dependencies are nil")
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 5000 {
		return nil, errors.New("content ingest batch size must be between 1 and 5000")
	}
	if cfg.CursorName == "" {
		cfg.CursorName = DefaultContentCursorName
	}
	if cfg.SourceSystem == "" {
		cfg.SourceSystem = DefaultContentSourceSystem
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ContentIngestService{unit: unit, source: source, cfg: cfg}, nil
}

func (s *ContentIngestService) IngestBatch(ctx context.Context) (ContentIngestResult, error) {
	var result ContentIngestResult
	var cursorValue string
	if err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Cursor == nil {
			return errors.New("content cursor repository is not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cfg.CursorName, s.cfg.SourceSystem)
		if err != nil {
			return err
		}
		cursorValue = cursor.Value
		return nil
	}); err != nil {
		return result, err
	}
	events, err := s.source.Fetch(ctx, cursorValue, s.cfg.BatchSize)
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

func (s *ContentIngestService) processOne(ctx context.Context, incoming ports.ContentEvent, result *ContentIngestResult) error {
	if incoming.SourceContentID == "" || incoming.ContentType == "" || incoming.AuthorUserID == 0 || incoming.SourceCreatedAt.IsZero() || incoming.CursorValue == "" || incoming.PayloadHash == "" {
		return errors.New("invalid normalized content event")
	}
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Content == nil || repos.Cursor == nil || repos.Conflict == nil {
			return errors.New("content ingest repositories are not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, s.cfg.CursorName, s.cfg.SourceSystem)
		if err != nil {
			return err
		}
		existing, err := repos.Content.FindCandidateBySource(ctx, s.cfg.SourceSystem, incoming.SourceContentID)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
		if existing != nil {
			if existing.PayloadHash != incoming.PayloadHash {
				if err := repos.Conflict.Create(ctx, ports.IngestConflict{SourceSystem: s.cfg.SourceSystem, SourceEventID: incoming.SourceContentID, ExistingPayloadHash: existing.PayloadHash, IncomingPayloadHash: incoming.PayloadHash, Reason: "same forum content has different payload"}); err != nil {
					return err
				}
				result.Conflicts++
			} else {
				result.Replayed++
			}
			return saveContentCursor(ctx, repos.Cursor, cursor, incoming)
		}
		periodID := uint64(0)
		if repos.Period != nil {
			activity, periodErr := repos.Period.FindActiveAt(ctx, incoming.SourceCreatedAt)
			if periodErr == nil {
				periodID = activity.ID
			} else if !errors.Is(periodErr, period.ErrNoActivePeriod) {
				return periodErr
			}
		}
		if _, err := repos.Content.CreateCandidate(ctx, ports.ContentCandidate{
			SourceSystem: s.cfg.SourceSystem, SourceContentID: incoming.SourceContentID, ContentType: incoming.ContentType,
			AuthorUserID: incoming.AuthorUserID, PeriodID: periodID, Title: incoming.Title, SourceCreatedAt: incoming.SourceCreatedAt,
			PayloadHash: incoming.PayloadHash, CursorValue: incoming.CursorValue, Status: ports.ContentCandidatePending, CreatedAt: s.cfg.Now(),
		}); err != nil {
			return err
		}
		result.Accepted++
		return saveContentCursor(ctx, repos.Cursor, cursor, incoming)
	})
}

func saveContentCursor(ctx context.Context, repository ports.CursorRepository, cursor ports.Cursor, event ports.ContentEvent) error {
	cursor.Value = event.CursorValue
	watermark := event.SourceCreatedAt
	cursor.WatermarkAt = &watermark
	if cursor.Version == math.MaxUint64 {
		return fmt.Errorf("content cursor version overflow")
	}
	cursor.Version++
	return repository.Save(ctx, cursor)
}
