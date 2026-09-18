package mysql

import (
	"context"
	"fmt"

	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type rewardAdminRepository struct{ db *gorm.DB }

func (r *rewardAdminRepository) lockDraft(ctx context.Context, periodID uint64) (periodModel, error) {
	var model periodModel
	if periodID == 0 {
		return model, ports.ErrConflict
	}
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", periodID).Take(&model).Error; err != nil {
		return model, err
	}
	if model.Status != string(period.StatusDraft) || model.FundingPolicy != period.VerifiedPaidFunding || model.TicketThresholdMilli <= 0 {
		return model, fmt.Errorf("%w: reward setup requires a verified-paid draft", ports.ErrConflict)
	}
	return model, nil
}

func (r *rewardAdminRepository) CreateDefinition(ctx context.Context, periodID uint64, definition reward.Definition) (reward.Definition, error) {
	if definition.ID != 0 || definition.RewardType != "newapi_quota" || definition.Amount <= 0 || definition.Weight == 0 || definition.TransferableQuota || !definition.Enabled || !validMySQLText(definition.RewardKey, 128) {
		return reward.Definition{}, fmt.Errorf("%w: invalid initial reward definition", ports.ErrConflict)
	}
	activity, err := r.lockDraft(ctx, periodID)
	if err != nil {
		return reward.Definition{}, err
	}
	if definition.ConfigVersion != activity.ConfigVersion {
		return reward.Definition{}, ports.ErrConflict
	}
	model := rewardDefinitionModel{PeriodID: periodID, RewardKey: definition.RewardKey, RewardType: definition.RewardType, Amount: definition.Amount, Weight: definition.Weight, ConfigVersion: definition.ConfigVersion, Enabled: true}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return reward.Definition{}, err
	}
	definition.ID = model.ID
	return definition, nil
}

func (r *rewardAdminRepository) CreateBudget(ctx context.Context, budget ports.RewardBudget) (ports.RewardBudget, error) {
	if budget.ID != 0 || budget.BudgetType != "loyalty" || budget.HardCap <= 0 || budget.ReservedAmount != 0 || budget.SettledAmount != 0 || budget.ReleasedAmount != 0 || budget.Version != 0 {
		return ports.RewardBudget{}, fmt.Errorf("%w: invalid initial reward budget", ports.ErrConflict)
	}
	if _, err := r.lockDraft(ctx, budget.PeriodID); err != nil {
		return ports.RewardBudget{}, err
	}
	model := rewardBudgetModel{PeriodID: budget.PeriodID, BudgetType: budget.BudgetType, HardCap: budget.HardCap}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return ports.RewardBudget{}, err
	}
	budget.ID = model.ID
	return budget, nil
}
