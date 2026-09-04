package ports

import (
	"context"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
)

type PeriodAdminRepository interface {
	ListDueForClose(ctx context.Context, now time.Time, limit int) ([]period.Period, error)
	FindByIDForUpdate(ctx context.Context, periodID uint64) (period.Period, error)
	Transition(ctx context.Context, periodID uint64, from, to period.Status, at time.Time) error
}

type AuditLog struct {
	ID           uint64
	ActorType    string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Reason       string
	BeforeJSON   []byte
	AfterJSON    []byte
	RequestID    string
	CreatedAt    time.Time
}

type AuditRepository interface {
	Append(ctx context.Context, log AuditLog) error
}

type ExperimentAssignment struct {
	ID           uint64
	ExperimentID string
	UserID       uint64
	Cohort       string
	BucketBps    uint16
	AssignedAt   time.Time
}

type ExperimentRepository interface {
	FindOrCreate(ctx context.Context, assignment ExperimentAssignment) (ExperimentAssignment, error)
}

// OperationalSnapshot is a point-in-time view used only for diagnostics and
// daily metrics. It is never an accounting source of truth.
type OperationalSnapshot struct {
	IngestLagSeconds     int64
	OpenConflictCount    int64
	LedgerMismatchCount  int64
	SettlementRetryCount int64
	SettlementDeadCount  int64
	BudgetReservedAmount int64
	BudgetHardCap        int64
}

type OperationsRepository interface {
	Snapshot(ctx context.Context, now time.Time, cursorName, sourceSystem string) (OperationalSnapshot, error)
}

type MetricValue struct {
	MetricDate    time.Time
	MetricName    string
	DimensionHash string
	Dimensions    []byte
	Value         int64
}

type MetricRepository interface {
	Upsert(ctx context.Context, metric MetricValue) error
}
