package ports

import (
	"context"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/economics"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
)

type UsageSource interface {
	Fetch(context.Context, string, int) ([]usage.Event, error)
}

type UsageRepository interface {
	FindBySource(ctx context.Context, sourceSystem, sourceEventID string) (*usage.Event, error)
	FindConsumeByRequest(ctx context.Context, userID uint64, requestID string) (*usage.Event, error)
	Create(ctx context.Context, event usage.Event) (usage.Event, error)
}

type IngestConflict struct {
	SourceSystem        string
	SourceEventID       string
	ExistingPayloadHash string
	IncomingPayloadHash string
	Reason              string
}

type ConflictRepository interface {
	Create(ctx context.Context, conflict IngestConflict) error
}

type Cursor struct {
	Name         string
	SourceSystem string
	Value        string
	WatermarkAt  *time.Time
	Version      uint64
}

type CursorRepository interface {
	GetOrCreateForUpdate(ctx context.Context, name, sourceSystem string) (Cursor, error)
	Save(ctx context.Context, cursor Cursor) error
}

type PeriodRepository interface {
	FindActiveAt(ctx context.Context, at time.Time) (period.Period, error)
}

type EconomicsRepository interface {
	ListRules(ctx context.Context, periodID uint64) ([]economics.Rule, error)
}

type UserPeriodStat struct {
	ID                   uint64
	UserID               uint64
	PeriodID             uint64
	NetContributionMilli int64
	EntitledTickets      int64
	SpentTickets         int64
	UsageEventCount      uint64
	Version              uint64
}

type UserPeriodStatRepository interface {
	GetOrCreateForUpdate(ctx context.Context, userID, periodID uint64) (UserPeriodStat, error)
	Save(ctx context.Context, stat UserPeriodStat) error
}
