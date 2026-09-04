package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxSettlementPayloadBytes   = 64 << 10
	maxIdempotencyResponseBytes = 64 << 10
)

type rewardDefinitionModel struct {
	ID                uint64 `gorm:"column:id;primaryKey"`
	PeriodID          uint64 `gorm:"column:period_id"`
	RewardKey         string `gorm:"column:reward_key"`
	RewardType        string `gorm:"column:reward_type"`
	Amount            int64  `gorm:"column:amount"`
	Weight            uint64 `gorm:"column:weight"`
	TransferableQuota bool   `gorm:"column:transferable_quota"`
	ConfigVersion     string `gorm:"column:config_version"`
	Enabled           bool   `gorm:"column:enabled"`
}

func (rewardDefinitionModel) TableName() string { return "pulse_reward_definition" }

type rewardBudgetModel struct {
	ID             uint64 `gorm:"column:id;primaryKey"`
	PeriodID       uint64 `gorm:"column:period_id"`
	BudgetType     string `gorm:"column:budget_type"`
	HardCap        int64  `gorm:"column:hard_cap"`
	ReservedAmount int64  `gorm:"column:reserved_amount"`
	SettledAmount  int64  `gorm:"column:settled_amount"`
	ReleasedAmount int64  `gorm:"column:released_amount"`
	Version        uint64 `gorm:"column:version"`
}

func (rewardBudgetModel) TableName() string { return "pulse_reward_budget" }

type rewardGrantModel struct {
	ID                 uint64     `gorm:"column:id;primaryKey"`
	GrantID            string     `gorm:"column:grant_id"`
	PeriodID           uint64     `gorm:"column:period_id"`
	UserID             uint64     `gorm:"column:user_id"`
	ActionID           string     `gorm:"column:action_id"`
	TriggerType        string     `gorm:"column:trigger_type"`
	RewardDefinitionID uint64     `gorm:"column:reward_definition_id"`
	RewardType         string     `gorm:"column:reward_type"`
	Amount             int64      `gorm:"column:amount"`
	RandomValue        string     `gorm:"column:random_value"`
	ConfigVersion      string     `gorm:"column:config_version"`
	Status             string     `gorm:"column:status"`
	SourceRef          string     `gorm:"column:source_ref"`
	Reason             string     `gorm:"column:reason"`
	BudgetType         string     `gorm:"column:budget_type"`
	SettledAt          *time.Time `gorm:"column:settled_at"`
	ReversedAt         *time.Time `gorm:"column:reversed_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
}

func (rewardGrantModel) TableName() string { return "pulse_reward_grant" }

type settlementOutboxModel struct {
	ID            uint64     `gorm:"column:id;primaryKey"`
	RewardGrantID uint64     `gorm:"column:reward_grant_id"`
	Operation     string     `gorm:"column:operation"`
	PayloadHash   string     `gorm:"column:payload_hash"`
	PayloadJSON   []byte     `gorm:"column:payload_json"`
	Status        string     `gorm:"column:status"`
	Attempts      uint32     `gorm:"column:attempts"`
	NextAttemptAt time.Time  `gorm:"column:next_attempt_at"`
	LeasedUntil   *time.Time `gorm:"column:leased_until"`
	LastError     string     `gorm:"column:last_error"`
	CompletedAt   *time.Time `gorm:"column:completed_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (settlementOutboxModel) TableName() string { return "pulse_settlement_outbox" }

type idempotencyModel struct {
	ID             uint64     `gorm:"column:id;primaryKey"`
	Scope          string     `gorm:"column:scope"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	PayloadHash    string     `gorm:"column:payload_hash"`
	ResponseStatus *int       `gorm:"column:response_status"`
	ResponseJSON   []byte     `gorm:"column:response_json"`
	ResourceType   string     `gorm:"column:resource_type"`
	ResourceID     string     `gorm:"column:resource_id"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
}

func (idempotencyModel) TableName() string { return "pulse_idempotency" }

type rewardRepository struct{ db *gorm.DB }
type idempotencyRepository struct{ db *gorm.DB }

func (r *rewardRepository) ListDefinitions(ctx context.Context, periodID uint64) ([]reward.Definition, error) {
	if periodID == 0 {
		return nil, errors.New("invalid reward definition period")
	}
	var models []rewardDefinitionModel
	if err := r.db.WithContext(ctx).Where("period_id = ? AND enabled = ?", periodID, true).Order("id ASC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list reward definitions: %w", err)
	}
	result := make([]reward.Definition, len(models))
	for i, model := range models {
		result[i] = reward.Definition{ID: model.ID, RewardKey: model.RewardKey, RewardType: model.RewardType, Amount: model.Amount, Weight: model.Weight, TransferableQuota: model.TransferableQuota, ConfigVersion: model.ConfigVersion, Enabled: model.Enabled}
	}
	return result, nil
}

func (r *rewardRepository) GetBudgetForUpdate(ctx context.Context, periodID uint64, budgetType string) (ports.RewardBudget, error) {
	if periodID == 0 || !validMySQLText(budgetType, 64) {
		return ports.RewardBudget{}, errors.New("invalid reward budget identity")
	}
	var model rewardBudgetModel
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("period_id = ? AND budget_type = ?", periodID, budgetType).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ports.RewardBudget{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.RewardBudget{}, fmt.Errorf("find reward budget: %w", err)
	}
	return rewardBudgetFromModel(model), nil
}

func (r *rewardRepository) SaveBudget(ctx context.Context, budget ports.RewardBudget) error {
	if err := validateRewardBudgetSave(budget); err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&rewardBudgetModel{}).Where("id = ? AND version = ?", budget.ID, budget.Version-1).Updates(map[string]any{
		"reserved_amount": budget.ReservedAmount, "settled_amount": budget.SettledAmount,
		"released_amount": budget.ReleasedAmount, "version": budget.Version,
	})
	if result.Error != nil {
		return fmt.Errorf("save reward budget: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *rewardRepository) ListGrantsForUser(ctx context.Context, userID uint64, limit int) ([]ports.RewardGrant, error) {
	if userID == 0 || limit <= 0 || limit > 100 {
		return nil, errors.New("invalid reward history query")
	}
	var models []rewardGrantModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC, id DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list reward history: %w", err)
	}
	result := make([]ports.RewardGrant, len(models))
	for i := range models {
		result[i] = rewardGrantFromModel(models[i])
	}
	return result, nil
}

func (r *rewardRepository) FindGrantByAction(ctx context.Context, periodID, userID uint64, actionID string) (*ports.RewardGrant, error) {
	if periodID == 0 || userID == 0 || !validMySQLText(actionID, 191) {
		return nil, errors.New("invalid reward grant action identity")
	}
	var model rewardGrantModel
	err := r.db.WithContext(ctx).Where("period_id = ? AND user_id = ? AND action_id = ?", periodID, userID, actionID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find reward grant: %w", err)
	}
	grant := rewardGrantFromModel(model)
	return &grant, nil
}

func (r *rewardRepository) FindGrantByID(ctx context.Context, grantID uint64) (*ports.RewardGrant, error) {
	return r.findGrantByID(ctx, grantID, false)
}

func (r *rewardRepository) FindGrantByIDForUpdate(ctx context.Context, grantID uint64) (*ports.RewardGrant, error) {
	return r.findGrantByID(ctx, grantID, true)
}

func (r *rewardRepository) findGrantByID(ctx context.Context, grantID uint64, lock bool) (*ports.RewardGrant, error) {
	if grantID == 0 {
		return nil, errors.New("invalid reward grant id")
	}
	var model rewardGrantModel
	query := r.db.WithContext(ctx)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Where("id = ?", grantID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find reward grant by id: %w", err)
	}
	grant := rewardGrantFromModel(model)
	return &grant, nil
}

func (r *rewardRepository) TransitionGrantStatus(ctx context.Context, grantID uint64, fromStatus, toStatus string, at time.Time) error {
	if grantID == 0 || at.IsZero() {
		return errors.New("invalid reward grant status transition")
	}
	updates := map[string]any{"status": toStatus}
	switch {
	case fromStatus == "pending" && toStatus == "settled":
		updates["settled_at"] = at
	case fromStatus == "settled" && toStatus == "reversed":
		// Keep settled_at as immutable history; reversal only appends its own
		// timestamp and moves the lifecycle state forward.
		updates["reversed_at"] = at
	default:
		return errors.New("invalid reward grant status transition")
	}
	result := r.db.WithContext(ctx).Model(&rewardGrantModel{}).
		Where("id = ? AND status = ?", grantID, fromStatus).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("transition reward grant status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func (r *rewardRepository) CreateGrant(ctx context.Context, grant ports.RewardGrant) (ports.RewardGrant, error) {
	if err := validateRewardGrantCreate(grant); err != nil {
		return ports.RewardGrant{}, err
	}
	model := rewardGrantModel{ID: grant.ID, GrantID: grant.GrantID, PeriodID: grant.PeriodID, UserID: grant.UserID, ActionID: grant.ActionID, TriggerType: grant.TriggerType, RewardDefinitionID: grant.RewardDefinitionID, RewardType: grant.RewardType, Amount: grant.Amount, RandomValue: grant.RandomValue, ConfigVersion: grant.ConfigVersion, Status: grant.Status, SourceRef: grant.SourceRef, Reason: grant.Reason, BudgetType: grant.BudgetType, SettledAt: grant.SettledAt, ReversedAt: grant.ReversedAt, CreatedAt: grant.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ports.RewardGrant{}, ports.ErrConflict
		}
		return ports.RewardGrant{}, fmt.Errorf("create reward grant: %w", err)
	}
	grant.ID = model.ID
	if grant.CreatedAt.IsZero() {
		grant.CreatedAt = model.CreatedAt
	}
	return grant, nil
}

func (r *rewardRepository) CreateOutbox(ctx context.Context, outbox ports.SettlementOutbox) (ports.SettlementOutbox, error) {
	if err := validateSettlementOutboxCreate(outbox); err != nil {
		return ports.SettlementOutbox{}, err
	}
	model := settlementOutboxModel{ID: outbox.ID, RewardGrantID: outbox.RewardGrantID, Operation: outbox.Operation, PayloadHash: outbox.PayloadHash, PayloadJSON: outbox.PayloadJSON, Status: outbox.Status, Attempts: outbox.Attempts, NextAttemptAt: outbox.NextAttemptAt, LeasedUntil: outbox.LeasedUntil, LastError: outbox.LastError, CompletedAt: outbox.CompletedAt, CreatedAt: outbox.CreatedAt}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return ports.SettlementOutbox{}, ports.ErrConflict
		}
		return ports.SettlementOutbox{}, fmt.Errorf("create settlement outbox: %w", err)
	}
	outbox.ID = model.ID
	if outbox.CreatedAt.IsZero() {
		outbox.CreatedAt = model.CreatedAt
	}
	return outbox, nil
}

func (r *idempotencyRepository) GetOrCreateForUpdate(ctx context.Context, scope, key, payloadHash string) (ports.IdempotencyRecord, error) {
	if err := validateIdempotencyIdentity(scope, key, payloadHash); err != nil {
		return ports.IdempotencyRecord{}, err
	}
	if err := r.db.WithContext(ctx).Exec(`
INSERT INTO pulse_idempotency (scope, idempotency_key, payload_hash)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, scope, key, payloadHash).Error; err != nil {
		return ports.IdempotencyRecord{}, fmt.Errorf("ensure idempotency record: %w", err)
	}
	var model idempotencyModel
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope = ? AND idempotency_key = ?", scope, key).Take(&model).Error; err != nil {
		return ports.IdempotencyRecord{}, fmt.Errorf("lock idempotency record: %w", err)
	}
	return idempotencyFromModel(model), nil
}

func (r *idempotencyRepository) Save(ctx context.Context, record ports.IdempotencyRecord) error {
	if err := validateIdempotencySave(record); err != nil {
		return err
	}
	// The request identity and first response are immutable. The row is normally
	// locked by GetOrCreateForUpdate, while this fence also prevents a stale or
	// corrupted record from completing a different request.
	result := r.db.WithContext(ctx).Model(&idempotencyModel{}).
		Where("id = ? AND scope = ? AND idempotency_key = ? AND payload_hash = ? AND response_status IS NULL", record.ID, record.Scope, record.Key, record.PayloadHash).
		Updates(map[string]any{
			"response_status": record.ResponseStatus, "response_json": record.ResponseJSON,
			"resource_type": record.ResourceType, "resource_id": record.ResourceID,
			"expires_at": record.ExpiresAt,
		})
	if result.Error != nil {
		return fmt.Errorf("save idempotency record: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

func rewardBudgetFromModel(model rewardBudgetModel) ports.RewardBudget {
	return ports.RewardBudget{ID: model.ID, PeriodID: model.PeriodID, BudgetType: model.BudgetType, HardCap: model.HardCap, ReservedAmount: model.ReservedAmount, SettledAmount: model.SettledAmount, ReleasedAmount: model.ReleasedAmount, Version: model.Version}
}

func rewardGrantFromModel(model rewardGrantModel) ports.RewardGrant {
	return ports.RewardGrant{ID: model.ID, GrantID: model.GrantID, PeriodID: model.PeriodID, UserID: model.UserID, ActionID: model.ActionID, TriggerType: model.TriggerType, RewardDefinitionID: model.RewardDefinitionID, RewardType: model.RewardType, Amount: model.Amount, RandomValue: model.RandomValue, ConfigVersion: model.ConfigVersion, Status: model.Status, SourceRef: model.SourceRef, Reason: model.Reason, BudgetType: model.BudgetType, SettledAt: model.SettledAt, ReversedAt: model.ReversedAt, CreatedAt: model.CreatedAt}
}

func idempotencyFromModel(model idempotencyModel) ports.IdempotencyRecord {
	return ports.IdempotencyRecord{ID: model.ID, Scope: model.Scope, Key: model.IdempotencyKey, PayloadHash: model.PayloadHash, ResponseStatus: model.ResponseStatus, ResponseJSON: model.ResponseJSON, ResourceType: model.ResourceType, ResourceID: model.ResourceID, ExpiresAt: model.ExpiresAt}
}

func (r *rewardRepository) FindOutboxByGrant(ctx context.Context, grantID uint64) (*ports.SettlementOutbox, error) {
	if grantID == 0 {
		return nil, errors.New("invalid settlement grant id")
	}
	var model settlementOutboxModel
	err := r.db.WithContext(ctx).Where("reward_grant_id = ?", grantID).Take(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ports.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find settlement outbox: %w", err)
	}
	outbox := settlementOutboxFromModel(model)
	return &outbox, nil
}

func (r *rewardRepository) ClaimDue(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]ports.SettlementOutbox, error) {
	if limit <= 0 || limit > 5000 {
		return nil, errors.New("settlement batch size must be between 1 and 5000")
	}
	if now.IsZero() || !leaseUntil.After(now) {
		return nil, errors.New("settlement lease must end after a non-zero claim time")
	}
	var models []settlementOutboxModel
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status IN ? AND next_attempt_at <= ? AND (leased_until IS NULL OR leased_until < ?)", []string{"pending", "retry", "processing"}, now, now).
		Order("id ASC").Limit(limit).Find(&models).Error
	if err != nil {
		return nil, fmt.Errorf("claim settlement outbox: %w", err)
	}
	result := make([]ports.SettlementOutbox, 0, len(models))
	for i := range models {
		model := &models[i]
		previousStatus := model.Status
		previousAttempts := model.Attempts
		nextAttempts, err := nextSettlementAttempt(previousAttempts)
		if err != nil {
			return nil, fmt.Errorf("claim settlement outbox %d: %w", model.ID, err)
		}
		model.Status = "processing"
		model.LeasedUntil = &leaseUntil
		model.Attempts = nextAttempts
		update := r.db.WithContext(ctx).Model(&settlementOutboxModel{}).
			Where("id = ? AND status = ? AND attempts = ?", model.ID, previousStatus, previousAttempts).
			Updates(map[string]any{
				"status": model.Status, "leased_until": model.LeasedUntil, "attempts": model.Attempts,
			})
		if update.Error != nil {
			return nil, fmt.Errorf("lease settlement outbox: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return nil, ports.ErrConflict
		}
		result = append(result, settlementOutboxFromModel(*model))
	}
	return result, nil
}

func nextSettlementAttempt(current uint32) (uint32, error) {
	if current == ^uint32(0) {
		return 0, errors.New("settlement attempt counter overflow")
	}
	return current + 1, nil
}

func (r *rewardRepository) ListForReconciliation(ctx context.Context, limit int) ([]ports.SettlementOutbox, error) {
	if limit <= 0 || limit > 5000 {
		return nil, errors.New("reconciliation batch size must be between 1 and 5000")
	}
	var models []settlementOutboxModel
	if err := r.db.WithContext(ctx).Where("status IN ?", []string{"processing", "retry", "dead"}).Order("id ASC").Limit(limit).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list reconciliation outbox: %w", err)
	}
	result := make([]ports.SettlementOutbox, len(models))
	for i, model := range models {
		result[i] = settlementOutboxFromModel(model)
	}
	return result, nil
}

func (r *rewardRepository) SaveOutbox(ctx context.Context, outbox ports.SettlementOutbox) error {
	if err := validateSettlementOutboxUpdate(outbox); err != nil {
		return err
	}
	// Attempts act as a durable lease fence. A worker that outlives its lease
	// must not be able to overwrite the result written by the worker that
	// reclaimed the row. Reconciliation may also complete retry/dead rows, so
	// all non-terminal source states are accepted here.
	result := r.db.WithContext(ctx).Model(&settlementOutboxModel{}).
		Where("id = ? AND attempts = ? AND status IN ?", outbox.ID, outbox.Attempts, []string{"processing", "retry", "dead"}).
		Updates(map[string]any{
			"status": outbox.Status, "attempts": outbox.Attempts, "next_attempt_at": outbox.NextAttemptAt,
			"leased_until": outbox.LeasedUntil, "last_error": outbox.LastError, "completed_at": outbox.CompletedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("save settlement outbox: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ports.ErrNotFound
	}
	return nil
}

func validateIdempotencyIdentity(scope, key, payloadHash string) error {
	if !validMySQLText(scope, 128) || !validMySQLText(key, 191) || len(payloadHash) != sha256.Size*2 {
		return fmt.Errorf("%w: invalid idempotency identity", ports.ErrConflict)
	}
	if _, err := hex.DecodeString(payloadHash); err != nil {
		return fmt.Errorf("%w: invalid idempotency payload hash", ports.ErrConflict)
	}
	return nil
}

func validateIdempotencySave(record ports.IdempotencyRecord) error {
	if record.ID == 0 {
		return fmt.Errorf("%w: invalid idempotency record", ports.ErrConflict)
	}
	if err := validateIdempotencyIdentity(record.Scope, record.Key, record.PayloadHash); err != nil {
		return err
	}
	if record.ResponseStatus == nil || *record.ResponseStatus < 200 || *record.ResponseStatus >= 300 ||
		len(record.ResponseJSON) == 0 || len(record.ResponseJSON) > maxIdempotencyResponseBytes ||
		!validMySQLText(record.ResourceType, 64) || !validMySQLText(record.ResourceID, 191) ||
		(record.ExpiresAt != nil && record.ExpiresAt.IsZero()) {
		return fmt.Errorf("%w: invalid completed idempotency state", ports.ErrConflict)
	}
	if _, err := canonicalSettlementPayloadHash(record.ResponseJSON); err != nil {
		return fmt.Errorf("%w: invalid idempotency response JSON", ports.ErrConflict)
	}
	return nil
}

func validateRewardBudgetSave(budget ports.RewardBudget) error {
	if budget.ID == 0 || budget.PeriodID == 0 || budget.Version == 0 || !validMySQLText(budget.BudgetType, 64) ||
		budget.HardCap < 0 || budget.ReservedAmount < 0 || budget.SettledAmount < 0 || budget.ReleasedAmount < 0 ||
		budget.SettledAmount > budget.HardCap || budget.ReservedAmount > budget.HardCap-budget.SettledAmount {
		return fmt.Errorf("%w: invalid reward budget", ports.ErrConflict)
	}
	return nil
}

func validateRewardGrantCreate(grant ports.RewardGrant) error {
	if grant.ID != 0 || grant.PeriodID == 0 || grant.UserID == 0 || grant.Amount <= 0 || grant.TransferableQuota ||
		grant.Status != "pending" || grant.GrantID != grant.SourceRef || grant.SettledAt != nil || grant.ReversedAt != nil || grant.CreatedAt.IsZero() ||
		!validMySQLText(grant.GrantID, 64) || !validMySQLText(grant.ActionID, 191) || !validMySQLText(grant.TriggerType, 32) ||
		!validMySQLText(grant.RewardType, 64) || !validMySQLText(grant.ConfigVersion, 64) || !validMySQLText(grant.SourceRef, 191) ||
		!validMySQLText(grant.Reason, 255) || !validMySQLText(grant.BudgetType, 64) || len(grant.RandomValue) != sha256.Size*2 {
		return fmt.Errorf("%w: invalid reward grant create state", ports.ErrConflict)
	}
	if _, err := hex.DecodeString(grant.RandomValue); err != nil {
		return fmt.Errorf("%w: invalid reward grant random value", ports.ErrConflict)
	}
	switch grant.TriggerType {
	case "pulse":
		if grant.RewardDefinitionID == 0 || grant.BudgetType != "loyalty" {
			return fmt.Errorf("%w: invalid pulse reward grant binding", ports.ErrConflict)
		}
	case "period_reward":
		if grant.RewardDefinitionID == 0 || grant.BudgetType != "period_reward" {
			return fmt.Errorf("%w: invalid period reward grant binding", ports.ErrConflict)
		}
	case "content":
		if grant.RewardDefinitionID != 0 || grant.BudgetType != "content_reward" {
			return fmt.Errorf("%w: invalid content reward grant binding", ports.ErrConflict)
		}
	default:
		return fmt.Errorf("%w: invalid reward grant trigger type", ports.ErrConflict)
	}
	return nil
}

func validMySQLText(value string, maxRunes int) bool {
	return value == strings.TrimSpace(value) && value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxRunes
}

func validateSettlementOutboxCreate(outbox ports.SettlementOutbox) error {
	if outbox.ID != 0 || outbox.RewardGrantID == 0 || outbox.Operation != "grant" ||
		(outbox.Status != "pending" && outbox.Status != "shadow") || outbox.Attempts != 0 ||
		outbox.NextAttemptAt.IsZero() || outbox.CreatedAt.IsZero() || outbox.LeasedUntil != nil ||
		strings.TrimSpace(outbox.LastError) != "" || outbox.CompletedAt != nil {
		return fmt.Errorf("%w: invalid settlement outbox create state", ports.ErrConflict)
	}
	if len(outbox.PayloadJSON) == 0 || len(outbox.PayloadJSON) > maxSettlementPayloadBytes {
		return fmt.Errorf("%w: invalid settlement outbox payload size", ports.ErrConflict)
	}
	canonicalHash, err := canonicalSettlementPayloadHash(outbox.PayloadJSON)
	if err != nil || outbox.PayloadHash != canonicalHash {
		return fmt.Errorf("%w: invalid settlement outbox payload hash", ports.ErrConflict)
	}
	return nil
}

func validateSettlementOutboxUpdate(outbox ports.SettlementOutbox) error {
	if outbox.ID == 0 || outbox.RewardGrantID == 0 || outbox.Attempts == 0 || outbox.NextAttemptAt.IsZero() || outbox.LeasedUntil != nil {
		return fmt.Errorf("%w: invalid settlement outbox update state", ports.ErrConflict)
	}
	switch outbox.Status {
	case "retry", "dead", "conflict":
		if strings.TrimSpace(outbox.LastError) == "" || outbox.CompletedAt != nil {
			return fmt.Errorf("%w: invalid failed settlement outbox state", ports.ErrConflict)
		}
	case "completed":
		if outbox.CompletedAt == nil || outbox.CompletedAt.IsZero() || outbox.LastError != "" {
			return fmt.Errorf("%w: invalid completed settlement outbox state", ports.ErrConflict)
		}
	default:
		return fmt.Errorf("%w: invalid settlement outbox target status", ports.ErrConflict)
	}
	return nil
}

func canonicalSettlementPayloadHash(payload []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", errors.New("trailing settlement JSON value")
		}
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func settlementOutboxFromModel(model settlementOutboxModel) ports.SettlementOutbox {
	return ports.SettlementOutbox{ID: model.ID, RewardGrantID: model.RewardGrantID, Operation: model.Operation, PayloadHash: model.PayloadHash, PayloadJSON: model.PayloadJSON, Status: model.Status, Attempts: model.Attempts, NextAttemptAt: model.NextAttemptAt, LeasedUntil: model.LeasedUntil, LastError: model.LastError, CompletedAt: model.CompletedAt, CreatedAt: model.CreatedAt}
}
