package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	if budget.ID == 0 || budget.Version == 0 {
		return fmt.Errorf("%w: invalid reward budget version", ports.ErrConflict)
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

func (r *rewardRepository) FindGrantByAction(ctx context.Context, periodID, userID uint64, actionID string) (*ports.RewardGrant, error) {
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

func (r *rewardRepository) CreateGrant(ctx context.Context, grant ports.RewardGrant) (ports.RewardGrant, error) {
	model := rewardGrantModel{ID: grant.ID, GrantID: grant.GrantID, PeriodID: grant.PeriodID, UserID: grant.UserID, ActionID: grant.ActionID, TriggerType: grant.TriggerType, RewardDefinitionID: grant.RewardDefinitionID, RewardType: grant.RewardType, Amount: grant.Amount, RandomValue: grant.RandomValue, ConfigVersion: grant.ConfigVersion, Status: grant.Status, SourceRef: grant.SourceRef, Reason: grant.Reason, SettledAt: grant.SettledAt, ReversedAt: grant.ReversedAt, CreatedAt: grant.CreatedAt}
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
	if scope == "" || key == "" || payloadHash == "" {
		return ports.IdempotencyRecord{}, fmt.Errorf("invalid idempotency identity")
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
	if record.ID == 0 {
		return fmt.Errorf("%w: invalid idempotency record", ports.ErrConflict)
	}
	result := r.db.WithContext(ctx).Model(&idempotencyModel{}).Where("id = ?", record.ID).Updates(map[string]any{
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
	return ports.RewardGrant{ID: model.ID, GrantID: model.GrantID, PeriodID: model.PeriodID, UserID: model.UserID, ActionID: model.ActionID, TriggerType: model.TriggerType, RewardDefinitionID: model.RewardDefinitionID, RewardType: model.RewardType, Amount: model.Amount, RandomValue: model.RandomValue, ConfigVersion: model.ConfigVersion, Status: model.Status, SourceRef: model.SourceRef, Reason: model.Reason, SettledAt: model.SettledAt, ReversedAt: model.ReversedAt, CreatedAt: model.CreatedAt}
}

func idempotencyFromModel(model idempotencyModel) ports.IdempotencyRecord {
	return ports.IdempotencyRecord{ID: model.ID, Scope: model.Scope, Key: model.IdempotencyKey, PayloadHash: model.PayloadHash, ResponseStatus: model.ResponseStatus, ResponseJSON: model.ResponseJSON, ResourceType: model.ResourceType, ResourceID: model.ResourceID, ExpiresAt: model.ExpiresAt}
}
