package mysql

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxAuditJSONBytes         = 64 << 10
	maxMetricDimensionsBytes  = 64 << 10
	emptyMetricDimensionsHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
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

func (r *periodAdminRepository) FindByIDForUpdate(ctx context.Context, periodID uint64) (period.Period, error) {
	if periodID == 0 {
		return period.Period{}, errors.New("invalid period id")
	}
	var model periodModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", periodID).Take(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return period.Period{}, ports.ErrNotFound
		}
		return period.Period{}, fmt.Errorf("find period for close: %w", err)
	}
	return model.toDomain(), nil
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
	if err := validateAuditLogCreate(log); err != nil {
		return err
	}
	model := auditLogModel{ID: log.ID, ActorType: log.ActorType, ActorID: log.ActorID, Action: log.Action, ResourceType: log.ResourceType, ResourceID: log.ResourceID, Reason: log.Reason, BeforeJSON: log.BeforeJSON, AfterJSON: log.AfterJSON, RequestID: log.RequestID, CreatedAt: log.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("append audit log: %w", err)
	}
	return nil
}

func validateAuditLogCreate(log ports.AuditLog) error {
	if log.ID != 0 || log.CreatedAt.IsZero() ||
		!validMySQLText(log.ActorType, 32) || !validMySQLText(log.ActorID, 128) ||
		!validMySQLText(log.Action, 128) || !validMySQLText(log.ResourceType, 64) ||
		!validMySQLText(log.ResourceID, 191) || !validMySQLText(log.Reason, 500) ||
		(log.RequestID != "" && !validMySQLText(log.RequestID, 128)) {
		return fmt.Errorf("%w: invalid audit log create state", ports.ErrConflict)
	}
	for _, document := range [][]byte{log.BeforeJSON, log.AfterJSON} {
		if len(document) == 0 {
			continue
		}
		if len(document) > maxAuditJSONBytes {
			return fmt.Errorf("%w: audit JSON exceeds %d bytes", ports.ErrConflict, maxAuditJSONBytes)
		}
		if _, err := canonicalSettlementPayloadHash(document); err != nil {
			return fmt.Errorf("%w: invalid audit JSON", ports.ErrConflict)
		}
	}
	return nil
}

func (r *experimentRepository) FindOrCreate(ctx context.Context, assignment ports.ExperimentAssignment) (ports.ExperimentAssignment, error) {
	if err := validateExperimentAssignmentCreate(assignment); err != nil {
		return ports.ExperimentAssignment{}, err
	}
	candidate := experimentAssignmentModel{ExperimentID: assignment.ExperimentID, UserID: assignment.UserID, Cohort: assignment.Cohort, BucketBps: assignment.BucketBps, AssignedAt: assignment.AssignedAt}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "experiment_id"}, {Name: "user_id"}}, DoNothing: true}).Create(&candidate).Error; err != nil {
		return ports.ExperimentAssignment{}, fmt.Errorf("save experiment assignment: %w", err)
	}
	// Always read into a fresh model. On duplicate-key no-op, MySQL/GORM may
	// leave the candidate primary key populated from driver state; reusing it
	// could accidentally add an unrelated id predicate and hide the historical
	// assignment that must remain immutable.
	var persisted experimentAssignmentModel
	if err := r.db.WithContext(ctx).Where("experiment_id = ? AND user_id = ?", assignment.ExperimentID, assignment.UserID).Take(&persisted).Error; err != nil {
		return ports.ExperimentAssignment{}, fmt.Errorf("read experiment assignment: %w", err)
	}
	if persisted.ID == 0 || persisted.ExperimentID != assignment.ExperimentID || persisted.UserID != assignment.UserID ||
		!validMySQLText(persisted.Cohort, 32) || persisted.BucketBps >= 10000 || persisted.AssignedAt.IsZero() {
		return ports.ExperimentAssignment{}, fmt.Errorf("%w: invalid persisted experiment assignment", ports.ErrConflict)
	}
	return ports.ExperimentAssignment{ID: persisted.ID, ExperimentID: persisted.ExperimentID, UserID: persisted.UserID, Cohort: persisted.Cohort, BucketBps: persisted.BucketBps, AssignedAt: persisted.AssignedAt}, nil
}

func validateExperimentAssignmentCreate(assignment ports.ExperimentAssignment) error {
	if assignment.ID != 0 || assignment.UserID == 0 || assignment.BucketBps >= 10000 || assignment.AssignedAt.IsZero() ||
		!validMySQLText(assignment.ExperimentID, 128) || !validMySQLText(assignment.Cohort, 32) {
		return fmt.Errorf("%w: invalid experiment assignment create state", ports.ErrConflict)
	}
	return nil
}

func (r *metricRepository) Upsert(ctx context.Context, metric ports.MetricValue) error {
	if err := validateMetricUpsert(metric); err != nil {
		return err
	}
	metricDate := time.Date(metric.MetricDate.Year(), metric.MetricDate.Month(), metric.MetricDate.Day(), 0, 0, 0, 0, metric.MetricDate.Location())
	dimensions := append([]byte(nil), metric.Dimensions...)
	model := metricDailyModel{MetricDate: metricDate, MetricName: metric.MetricName, DimensionHash: metric.DimensionHash, Dimensions: dimensions, Value: metric.Value}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "metric_date"}, {Name: "metric_name"}, {Name: "dimension_hash"}}, DoUpdates: clause.Assignments(map[string]any{"dimensions_json": dimensions, "metric_value": metric.Value})}).Create(&model).Error; err != nil {
		return fmt.Errorf("upsert daily metric: %w", err)
	}
	return nil
}

func validateMetricUpsert(metric ports.MetricValue) error {
	year := metric.MetricDate.Year()
	if metric.MetricDate.IsZero() || year < 1000 || year > 9999 || !validMySQLText(metric.MetricName, 128) ||
		len(metric.DimensionHash) != 64 || metric.DimensionHash != strings.ToLower(metric.DimensionHash) {
		return fmt.Errorf("%w: invalid daily metric identity", ports.ErrConflict)
	}
	if !validLowerHex(metric.DimensionHash, 64) {
		return fmt.Errorf("%w: invalid daily metric dimension hash", ports.ErrConflict)
	}
	if len(metric.Dimensions) == 0 {
		if metric.DimensionHash != emptyMetricDimensionsHash {
			return fmt.Errorf("%w: empty metric dimensions hash mismatch", ports.ErrConflict)
		}
		return nil
	}
	if len(metric.Dimensions) > maxMetricDimensionsBytes {
		return fmt.Errorf("%w: metric dimensions exceed %d bytes", ports.ErrConflict, maxMetricDimensionsBytes)
	}
	canonicalHash, err := canonicalSettlementPayloadHash(metric.Dimensions)
	if err != nil || canonicalHash != metric.DimensionHash {
		return fmt.Errorf("%w: invalid metric dimensions JSON or hash", ports.ErrConflict)
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
WHERE status IN ('retry', 'dead', 'conflict')
GROUP BY status`).Scan(&settlementRows).Error; err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("count settlement backlog: %w", err)
	}
	for _, row := range settlementRows {
		switch row.Status {
		case "retry":
			snapshot.SettlementRetryCount = row.Count
		case "dead", "conflict":
			snapshot.SettlementDeadCount += row.Count
		}
	}
	var reservedRaw, hardCapRaw string
	if err := r.db.WithContext(ctx).Raw(`
SELECT COALESCE(SUM(reserved_amount), 0), COALESCE(SUM(hard_cap), 0)
FROM pulse_reward_budget`).Row().Scan(&reservedRaw, &hardCapRaw); err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("read budget usage: %w", err)
	}
	var err error
	if snapshot.BudgetReservedAmount, err = parseAggregateInt64(reservedRaw); err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("read budget reserved amount: %w", err)
	}
	if snapshot.BudgetHardCap, err = parseAggregateInt64(hardCapRaw); err != nil {
		return ports.OperationalSnapshot{}, fmt.Errorf("read budget hard cap: %w", err)
	}
	return snapshot, nil
}

type contentCandidateModel struct {
	ID              uint64     `gorm:"column:id;primaryKey"`
	SourceSystem    string     `gorm:"column:source_system"`
	SourceContentID string     `gorm:"column:source_content_id"`
	ContentType     string     `gorm:"column:content_type"`
	AuthorUserID    uint64     `gorm:"column:author_user_id"`
	PeriodID        uint64     `gorm:"column:period_id"`
	Title           string     `gorm:"column:title"`
	SourceCreatedAt time.Time  `gorm:"column:source_created_at"`
	PayloadHash     string     `gorm:"column:payload_hash"`
	CursorValue     string     `gorm:"column:cursor_value"`
	Status          string     `gorm:"column:status"`
	ReviewActorType string     `gorm:"column:review_actor_type"`
	ReviewActorID   string     `gorm:"column:review_actor_id"`
	ReviewReason    string     `gorm:"column:review_reason"`
	ReviewedAt      *time.Time `gorm:"column:reviewed_at"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
}

func (contentCandidateModel) TableName() string { return "pulse_content_candidate" }

type contentAwardModel struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	CandidateID  uint64    `gorm:"column:candidate_id"`
	AwardVersion uint64    `gorm:"column:award_version"`
	ActionID     string    `gorm:"column:action_id"`
	PeriodID     uint64    `gorm:"column:period_id"`
	UserID       uint64    `gorm:"column:user_id"`
	Amount       int64     `gorm:"column:amount"`
	RewardType   string    `gorm:"column:reward_type"`
	BudgetType   string    `gorm:"column:budget_type"`
	GrantID      string    `gorm:"column:grant_id"`
	Status       string    `gorm:"column:status"`
	Reason       string    `gorm:"column:reason"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (contentAwardModel) TableName() string { return "pulse_content_award" }

type contentAwardLimitGuardModel struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	ScopeKey  string    `gorm:"column:scope_key"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (contentAwardLimitGuardModel) TableName() string { return "pulse_content_award_limit_guard" }

type contentRepository struct{ db *gorm.DB }

func contentCandidateFromModel(model contentCandidateModel) ports.ContentCandidate {
	return ports.ContentCandidate{ID: model.ID, SourceSystem: model.SourceSystem, SourceContentID: model.SourceContentID, ContentType: model.ContentType, AuthorUserID: model.AuthorUserID, PeriodID: model.PeriodID, Title: model.Title, SourceCreatedAt: model.SourceCreatedAt, PayloadHash: model.PayloadHash, CursorValue: model.CursorValue, Status: model.Status, ReviewActorType: model.ReviewActorType, ReviewActorID: model.ReviewActorID, ReviewReason: model.ReviewReason, ReviewedAt: model.ReviewedAt, CreatedAt: model.CreatedAt}
}

func contentAwardFromModel(model contentAwardModel) ports.ContentAward {
	return ports.ContentAward{ID: model.ID, CandidateID: model.CandidateID, AwardVersion: model.AwardVersion, ActionID: model.ActionID, PeriodID: model.PeriodID, UserID: model.UserID, Amount: model.Amount, RewardType: model.RewardType, BudgetType: model.BudgetType, GrantID: model.GrantID, Status: model.Status, Reason: model.Reason, CreatedAt: model.CreatedAt}
}

func (r *contentRepository) FindCandidateBySource(ctx context.Context, sourceSystem, sourceContentID string) (*ports.ContentCandidate, error) {
	if !validMySQLText(sourceSystem, 64) || !validMySQLText(sourceContentID, 191) {
		return nil, fmt.Errorf("%w: invalid content candidate source identity", ports.ErrConflict)
	}
	var model contentCandidateModel
	err := r.db.WithContext(ctx).Where("source_system = ? AND source_content_id = ?", sourceSystem, sourceContentID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find content candidate: %w", err)
	}
	candidate := contentCandidateFromModel(model)
	return &candidate, nil
}

func (r *contentRepository) FindCandidateForUpdate(ctx context.Context, candidateID uint64) (*ports.ContentCandidate, error) {
	if candidateID == 0 {
		return nil, fmt.Errorf("%w: invalid content candidate id", ports.ErrConflict)
	}
	var model contentCandidateModel
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", candidateID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock content candidate: %w", err)
	}
	candidate := contentCandidateFromModel(model)
	return &candidate, nil
}

func (r *contentRepository) CreateCandidate(ctx context.Context, candidate ports.ContentCandidate) (ports.ContentCandidate, error) {
	if err := validateContentCandidateCreate(candidate); err != nil {
		return ports.ContentCandidate{}, err
	}
	model := contentCandidateModel{SourceSystem: candidate.SourceSystem, SourceContentID: candidate.SourceContentID, ContentType: candidate.ContentType, AuthorUserID: candidate.AuthorUserID, PeriodID: candidate.PeriodID, Title: candidate.Title, SourceCreatedAt: candidate.SourceCreatedAt, PayloadHash: candidate.PayloadHash, CursorValue: candidate.CursorValue, Status: candidate.Status, CreatedAt: candidate.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ports.ContentCandidate{}, ports.ErrConflict
		}
		return ports.ContentCandidate{}, fmt.Errorf("create content candidate: %w", err)
	}
	candidate.ID = model.ID
	return candidate, nil
}

func validateContentCandidateCreate(candidate ports.ContentCandidate) error {
	if candidate.ID != 0 || candidate.AuthorUserID == 0 || candidate.Status != ports.ContentCandidatePending ||
		!validMySQLText(candidate.SourceSystem, 64) || !validMySQLText(candidate.SourceContentID, 191) ||
		!validMySQLText(candidate.ContentType, 64) || !validOptionalMySQLString(candidate.Title, 500) ||
		!validMySQLText(candidate.CursorValue, 191) || !validLowerHex(candidate.PayloadHash, 64) ||
		!validMySQLTime(candidate.SourceCreatedAt) || !validMySQLTime(candidate.CreatedAt) ||
		candidate.ReviewActorType != "" || candidate.ReviewActorID != "" || candidate.ReviewReason != "" || candidate.ReviewedAt != nil {
		return fmt.Errorf("%w: invalid content candidate create state", ports.ErrConflict)
	}
	return nil
}

func (r *contentRepository) ReviewCandidate(ctx context.Context, candidateID uint64, status, actorType, actorID, reason string, reviewedAt time.Time) error {
	if candidateID == 0 || !validContentCandidateReviewStatus(status) || !validMySQLText(actorType, 32) ||
		!validMySQLText(actorID, 128) || !validMySQLText(reason, 500) || !validMySQLTime(reviewedAt) {
		return fmt.Errorf("%w: invalid content candidate review", ports.ErrConflict)
	}
	result := r.db.WithContext(ctx).Model(&contentCandidateModel{}).
		Where("id = ? AND status = ? AND reviewed_at IS NULL", candidateID, ports.ContentCandidatePending).
		Updates(map[string]any{"status": status, "review_actor_type": actorType, "review_actor_id": actorID, "review_reason": reason, "reviewed_at": reviewedAt})
	if result.Error != nil {
		return fmt.Errorf("review content candidate: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func validContentCandidateReviewStatus(status string) bool {
	switch status {
	case ports.ContentCandidateApproved, ports.ContentCandidateRejected, ports.ContentCandidateDeleted:
		return true
	default:
		return false
	}
}

func (r *contentRepository) FindAwardByAction(ctx context.Context, actionID string) (*ports.ContentAward, error) {
	return r.findAwardByAction(ctx, actionID, false)
}

func (r *contentRepository) FindAwardByActionForUpdate(ctx context.Context, actionID string) (*ports.ContentAward, error) {
	return r.findAwardByAction(ctx, actionID, true)
}

func (r *contentRepository) findAwardByAction(ctx context.Context, actionID string, lock bool) (*ports.ContentAward, error) {
	var model contentAwardModel
	query := r.db.WithContext(ctx)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("action_id = ?", actionID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find content award: %w", err)
	}
	award := contentAwardFromModel(model)
	return &award, nil
}

func (r *contentRepository) CreateAward(ctx context.Context, award ports.ContentAward) (ports.ContentAward, error) {
	if award.CandidateID == 0 || award.AwardVersion == 0 || award.ActionID == "" || award.PeriodID == 0 || award.UserID == 0 || award.Amount < 0 || award.RewardType == "" || award.BudgetType == "" || award.Status == "" || award.Reason == "" {
		return ports.ContentAward{}, errors.New("invalid content award")
	}
	model := contentAwardModel{ID: award.ID, CandidateID: award.CandidateID, AwardVersion: award.AwardVersion, ActionID: award.ActionID, PeriodID: award.PeriodID, UserID: award.UserID, Amount: award.Amount, RewardType: award.RewardType, BudgetType: award.BudgetType, GrantID: award.GrantID, Status: award.Status, Reason: award.Reason, CreatedAt: award.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ports.ContentAward{}, ports.ErrConflict
		}
		return ports.ContentAward{}, fmt.Errorf("create content award: %w", err)
	}
	award.ID = model.ID
	if award.CreatedAt.IsZero() {
		award.CreatedAt = model.CreatedAt
	}
	return award, nil
}

func (r *contentRepository) TransitionAwardStatus(ctx context.Context, actionID, fromStatus, toStatus string) error {
	if actionID == "" || fromStatus != ports.ContentAwardSettled || toStatus != ports.ContentAwardReversed {
		return errors.New("invalid content award status transition")
	}
	result := r.db.WithContext(ctx).Model(&contentAwardModel{}).
		Where("action_id = ? AND status = ?", actionID, fromStatus).
		Updates(map[string]any{"status": toStatus})
	if result.Error != nil {
		return fmt.Errorf("transition content award status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *contentRepository) MarkAwardSettledByGrantID(ctx context.Context, grantID string) error {
	if grantID == "" {
		return errors.New("invalid content grant id")
	}
	// A loyalty grant has no content award, and a repeated settlement may
	// already have moved this row. Both cases are safe no-ops. Only pending
	// awards can transition to settled; reversed awards must never be revived.
	if err := r.db.WithContext(ctx).Model(&contentAwardModel{}).Where("grant_id = ? AND status = ?", grantID, ports.ContentAwardPending).Updates(map[string]any{"status": ports.ContentAwardSettled}).Error; err != nil {
		return fmt.Errorf("mark content award settled: %w", err)
	}
	return nil
}

func (r *contentRepository) LockAwardLimits(ctx context.Context, userID, periodID uint64, day time.Time) error {
	if userID == 0 || periodID == 0 || day.IsZero() {
		return errors.New("invalid content award limit scope")
	}
	// Content awards are manually reviewed and low throughput. One seeded row
	// serializes all cap checks, avoiding dynamic-row insert/gap-lock deadlocks
	// while making both user-period and cross-period daily totals race-free.
	var locked contentAwardLimitGuardModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_key = ?", "global").Take(&locked).Error; err != nil {
		return fmt.Errorf("lock content award limit guard: %w", err)
	}
	return nil
}

func (r *contentRepository) SumUserActiveAwards(ctx context.Context, userID, periodID uint64) (int64, error) {
	var raw string
	if err := r.db.WithContext(ctx).Model(&contentAwardModel{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND period_id = ? AND status IN ?", userID, periodID, []string{ports.ContentAwardPending, ports.ContentAwardSettled}).Select("COALESCE(SUM(amount), 0)").Row().Scan(&raw); err != nil {
		return 0, fmt.Errorf("sum user content awards: %w", err)
	}
	total, err := parseAggregateInt64(raw)
	if err != nil {
		return 0, fmt.Errorf("sum user content awards: %w", err)
	}
	return total, nil
}

func (r *contentRepository) SumDailyActiveAwards(ctx context.Context, day time.Time) (int64, error) {
	start, end := contentBusinessDayBounds(day)
	var raw string
	if err := r.db.WithContext(ctx).Model(&contentAwardModel{}).Clauses(clause.Locking{Strength: "UPDATE"}).Where("created_at >= ? AND created_at < ? AND status IN ?", start, end, []string{ports.ContentAwardPending, ports.ContentAwardSettled}).Select("COALESCE(SUM(amount), 0)").Row().Scan(&raw); err != nil {
		return 0, fmt.Errorf("sum daily content awards: %w", err)
	}
	total, err := parseAggregateInt64(raw)
	if err != nil {
		return 0, fmt.Errorf("sum daily content awards: %w", err)
	}
	return total, nil
}

func validOptionalMySQLString(value string, maxRunes int) bool {
	return maxRunes > 0 && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes
}

func validLowerHex(value string, length int) bool {
	if length <= 0 || len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validMySQLTime(value time.Time) bool {
	year := value.Year()
	return !value.IsZero() && year >= 1000 && year <= 9999
}

func parseAggregateInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("aggregate does not fit int64: %w", err)
	}
	return value, nil
}

func contentBusinessDayBounds(at time.Time) (time.Time, time.Time) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	local := at.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return start, start.AddDate(0, 0, 1)
}
