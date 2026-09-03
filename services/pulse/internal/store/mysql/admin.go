package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type auditLogModel struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	ActorType    string    `gorm:"column:actor_type"`
	ActorID      string    `gorm:"column:actor_id"`
	Action       string    `gorm:"column:action"`
	ResourceType string    `gorm:"column:resource_type"`
	ResourceID   string    `gorm:"column:resource_id"`
	Reason       string    `gorm:"column:reason"`
	BeforeJSON   []byte    `gorm:"column:before_json"`
	AfterJSON    []byte    `gorm:"column:after_json"`
	RequestID    string    `gorm:"column:request_id"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (auditLogModel) TableName() string { return "pulse_audit_log" }

type experimentAssignmentModel struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	ExperimentID string    `gorm:"column:experiment_id"`
	UserID       uint64    `gorm:"column:user_id"`
	Cohort       string    `gorm:"column:cohort"`
	BucketBps    uint16    `gorm:"column:bucket_bps"`
	AssignedAt   time.Time `gorm:"column:assigned_at"`
}

func (experimentAssignmentModel) TableName() string { return "pulse_experiment_assignment" }

type metricDailyModel struct {
	ID            uint64    `gorm:"column:id;primaryKey"`
	MetricDate    time.Time `gorm:"column:metric_date"`
	MetricName    string    `gorm:"column:metric_name"`
	DimensionHash string    `gorm:"column:dimension_hash"`
	Dimensions    []byte    `gorm:"column:dimensions_json"`
	Value         int64     `gorm:"column:metric_value"`
}

func (metricDailyModel) TableName() string { return "pulse_metric_daily" }

type periodAdminRepository struct{ db *gorm.DB }
type auditRepository struct{ db *gorm.DB }
type experimentRepository struct{ db *gorm.DB }
type metricRepository struct{ db *gorm.DB }

func (r *periodAdminRepository) ListDueForClose(ctx context.Context, now time.Time, limit int) ([]period.Period, error) {
	if limit <= 0 || limit > 5000 {
		return nil, errors.New("period close batch size must be between 1 and 5000")
	}
	var models []periodModel
	if err := r.db.WithContext(ctx).Where("status IN ? AND ends_at <= ?", []string{string(period.StatusActive), string(period.StatusSettling)}, now).Order("ends_at ASC, id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list periods due for close: %w", err)
	}
	result := make([]period.Period, len(models))
	for i, model := range models {
		result[i] = model.toDomain()
	}
	return result, nil
}

func (r *periodAdminRepository) Transition(ctx context.Context, periodID uint64, from, to period.Status, at time.Time) error {
	if err := period.Transition(from, to); err != nil {
		return err
	}
	updates := map[string]any{"status": to}
	switch to {
	case period.StatusActive:
		updates["activated_at"] = at
	case period.StatusSettling:
		updates["settling_at"] = at
	case period.StatusClosed:
		updates["closed_at"] = at
	}
	result := r.db.WithContext(ctx).Model(&periodModel{}).Where("id = ? AND status = ?", periodID, from).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("transition period: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *auditRepository) Append(ctx context.Context, log ports.AuditLog) error {
	if log.ActorType == "" || log.ActorID == "" || log.Action == "" || log.ResourceType == "" || log.ResourceID == "" || log.Reason == "" {
		return errors.New("invalid audit log")
	}
	model := auditLogModel{ID: log.ID, ActorType: log.ActorType, ActorID: log.ActorID, Action: log.Action, ResourceType: log.ResourceType, ResourceID: log.ResourceID, Reason: log.Reason, BeforeJSON: log.BeforeJSON, AfterJSON: log.AfterJSON, RequestID: log.RequestID, CreatedAt: log.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

func (r *experimentRepository) FindOrCreate(ctx context.Context, assignment ports.ExperimentAssignment) (ports.ExperimentAssignment, error) {
	if assignment.ExperimentID == "" || assignment.UserID == 0 || assignment.Cohort == "" || assignment.BucketBps >= 10000 {
		return ports.ExperimentAssignment{}, errors.New("invalid experiment assignment")
	}
	model := experimentAssignmentModel{ID: assignment.ID, ExperimentID: assignment.ExperimentID, UserID: assignment.UserID, Cohort: assignment.Cohort, BucketBps: assignment.BucketBps, AssignedAt: assignment.AssignedAt}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "experiment_id"}, {Name: "user_id"}}, DoNothing: true}).Create(&model).Error; err != nil {
		return ports.ExperimentAssignment{}, fmt.Errorf("save experiment assignment: %w", err)
	}
	if err := r.db.WithContext(ctx).Where("experiment_id = ? AND user_id = ?", assignment.ExperimentID, assignment.UserID).Take(&model).Error; err != nil {
		return ports.ExperimentAssignment{}, fmt.Errorf("read experiment assignment: %w", err)
	}
	return ports.ExperimentAssignment{ID: model.ID, ExperimentID: model.ExperimentID, UserID: model.UserID, Cohort: model.Cohort, BucketBps: model.BucketBps, AssignedAt: model.AssignedAt}, nil
}

func (r *metricRepository) Upsert(ctx context.Context, metric ports.MetricValue) error {
	if metric.MetricName == "" || metric.DimensionHash == "" {
		return errors.New("invalid metric identity")
	}
	model := metricDailyModel{MetricDate: metric.MetricDate, MetricName: metric.MetricName, DimensionHash: metric.DimensionHash, Dimensions: metric.Dimensions, Value: metric.Value}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "metric_date"}, {Name: "metric_name"}, {Name: "dimension_hash"}}, DoUpdates: clause.Assignments(map[string]any{"dimensions_json": metric.Dimensions, "metric_value": metric.Value})}).Create(&model).Error; err != nil {
		return fmt.Errorf("upsert daily metric: %w", err)
	}
	return nil
}

type operationsRepository struct{ db *gorm.DB }

// Snapshot reads operational diagnostics in one transaction-local view. These
// values are deliberately rebuilt from durable tables and are safe to lose;
// they must never be used to authorize an economic mutation.
func (r *operationsRepository) Snapshot(ctx context.Context, now time.Time, cursorName, sourceSystem string) (ports.OperationalSnapshot, error) {
	if r == nil || r.db == nil {
		return ports.OperationalSnapshot{}, errors.New("operations repository is not initialized")
	}
	if cursorName == "" || sourceSystem == "" || now.IsZero() {
		return ports.OperationalSnapshot{}, errors.New("invalid operations snapshot request")
	}
	var snapshot ports.OperationalSnapshot
	if err := r.db.WithContext(ctx).Raw(`
SELECT COALESCE(GREATEST(TIMESTAMPDIFF(SECOND, MAX(watermark_at), ?), 0), 0)
FROM pulse_worker_cursor
WHERE cursor_name = ? AND source_system = ?`, now, cursorName, sourceSystem).Scan(&snapshot.IngestLagSeconds).Error; err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("read ingest lag: %w", err)
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT COUNT(*) FROM pulse_ingest_conflict WHERE status = 'open'`).Scan(&snapshot.OpenConflictCount).Error; err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("count ingest conflicts: %w", err)
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT COUNT(*)
FROM pulse_account a
WHERE a.balance <> COALESCE((
    SELECT SUM(l.amount) FROM pulse_ledger_entry l
    WHERE l.user_id = a.user_id AND l.period_id = a.period_id AND l.asset_type = a.asset_type
), 0)
OR a.version <> COALESCE((
    SELECT COUNT(*) FROM pulse_ledger_entry l
    WHERE l.user_id = a.user_id AND l.period_id = a.period_id AND l.asset_type = a.asset_type
), 0)`).Scan(&snapshot.LedgerMismatchCount).Error; err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("count ledger mismatches: %w", err)
	}
	var settlementRows []struct {
		Status string
		Count  int64
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT status, COUNT(*) AS count
FROM pulse_settlement_outbox
WHERE status IN ('retry', 'dead')
GROUP BY status`).Scan(&settlementRows).Error; err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("count settlement backlog: %w", err)
	}
	for _, row := range settlementRows {
		switch row.Status {
		case "retry":
			snapshot.SettlementRetryCount = row.Count
		case "dead":
			snapshot.SettlementDeadCount = row.Count
		}
	}
	if err := r.db.WithContext(ctx).Raw(`
SELECT COALESCE(SUM(reserved_amount), 0), COALESCE(SUM(hard_cap), 0)
FROM pulse_reward_budget`).Row().Scan(&snapshot.BudgetReservedAmount, &snapshot.BudgetHardCap); err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("read budget usage: %w", err)
	}
	return snapshot, nil
}
