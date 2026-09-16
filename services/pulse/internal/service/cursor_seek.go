package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// CursorSeekCommand moves the ingest cursor forward without ingesting the
// rows it skips. Those rows are permanently forfeited for accounting: they
// will never produce a usage event, contribution, ticket or reward.
type CursorSeekCommand struct {
	ActorType    string
	ActorID      string
	CursorName   string
	SourceSystem string
	// SkipBefore is the source event time to resume from. The cursor is set to
	// the position immediately before it, so a row at exactly this instant is
	// still ingested.
	SkipBefore time.Time
	Reason     string
}

type CursorSeekResult struct {
	CursorName  string     `json:"cursor_name"`
	Before      string     `json:"before"`
	After       string     `json:"after"`
	WatermarkAt *time.Time `json:"watermark_at"`
}

type CursorSeekService struct {
	unit ports.UnitOfWork
	now  func() time.Time
}

func NewCursorSeekService(unit ports.UnitOfWork, now func() time.Time) (*CursorSeekService, error) {
	if unit == nil {
		return nil, errors.New("cursor seek unit of work is nil")
	}
	if now == nil {
		now = time.Now
	}
	return &CursorSeekService{unit: unit, now: now}, nil
}

// Seek only ever moves the cursor forward. Moving it backwards would re-read
// already-ingested rows; they are idempotent by source_event_id, but a
// backwards cursor also rewinds the watermark that period close depends on.
func (s *CursorSeekService) Seek(ctx context.Context, command CursorSeekCommand) (CursorSeekResult, error) {
	command, err := normalizeCursorSeekCommand(command)
	if err != nil {
		return CursorSeekResult{}, err
	}
	// The log cursor is "<unix seconds>:<id>". Setting the id to the maximum
	// for the preceding second resumes at the first row of SkipBefore itself.
	target := fmt.Sprintf("%d:%d", command.SkipBefore.Unix()-1, int64(^uint64(0)>>1))

	var result CursorSeekResult
	err = s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Cursor == nil || repos.Audit == nil {
			return errors.New("cursor seek repositories are not initialized")
		}
		cursor, err := repos.Cursor.GetOrCreateForUpdate(ctx, command.CursorName, command.SourceSystem)
		if err != nil {
			return err
		}
		if !logCursorLess(cursor.Value, target) {
			return fmt.Errorf("%w: cursor %s is already at or past %s", ports.ErrConflict, cursor.Value, target)
		}
		before := cursor.Value
		watermark := command.SkipBefore.Add(-time.Second)
		cursor.Value = target
		cursor.WatermarkAt = &watermark
		cursor.Version++
		if err := repos.Cursor.Save(ctx, cursor); err != nil {
			return err
		}
		afterJSON, err := json.Marshal(struct {
			CursorName  string    `json:"cursor_name"`
			Before      string    `json:"before"`
			After       string    `json:"after"`
			WatermarkAt time.Time `json:"watermark_at"`
		}{command.CursorName, before, target, watermark})
		if err != nil {
			return err
		}
		if err := repos.Audit.Append(ctx, ports.AuditLog{
			ActorType: command.ActorType, ActorID: command.ActorID, Action: "cursor_seek",
			ResourceType: "worker_cursor", ResourceID: command.CursorName, Reason: command.Reason,
			AfterJSON: afterJSON, CreatedAt: s.now(),
		}); err != nil {
			return err
		}
		result = CursorSeekResult{CursorName: command.CursorName, Before: before, After: target, WatermarkAt: &watermark}
		return nil
	})
	if err != nil {
		return CursorSeekResult{}, err
	}
	return result, nil
}

// logCursorLess compares "<created_at>:<id>" cursors componentwise. String
// comparison would be wrong: "999:1" sorts after "1000:1".
func logCursorLess(left, right string) bool {
	leftAt, leftID, leftOK := parseLogCursor(left)
	if !leftOK {
		// An empty or unparseable cursor is the beginning of time, so any
		// valid target is ahead of it.
		return true
	}
	rightAt, rightID, rightOK := parseLogCursor(right)
	if !rightOK {
		return false
	}
	if leftAt != rightAt {
		return leftAt < rightAt
	}
	return leftID < rightID
}

func parseLogCursor(value string) (int64, int64, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	createdAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return createdAt, id, true
}

func normalizeCursorSeekCommand(command CursorSeekCommand) (CursorSeekCommand, error) {
	command.ActorType = strings.TrimSpace(command.ActorType)
	command.ActorID = strings.TrimSpace(command.ActorID)
	command.CursorName = strings.TrimSpace(command.CursorName)
	command.SourceSystem = strings.TrimSpace(command.SourceSystem)
	command.Reason = strings.TrimSpace(command.Reason)

	if command.ActorType == "" || command.ActorID == "" {
		return command, errors.New("cursor seek actor is required")
	}
	if command.Reason == "" {
		return command, errors.New("cursor seek reason is required")
	}
	if command.CursorName == "" {
		command.CursorName = DefaultUsageCursorName
	}
	if command.SourceSystem == "" {
		command.SourceSystem = "new-api-log"
	}
	if command.SkipBefore.IsZero() {
		return command, errors.New("cursor seek target time is required")
	}
	if command.SkipBefore.Unix() <= 0 {
		return command, errors.New("cursor seek target time is out of range")
	}
	return command, nil
}
