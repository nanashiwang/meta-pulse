package mysql

import (
	"context"
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm"
)

type rewardFeedRepository struct{ db *gorm.DB }

func (r *rewardFeedRepository) ListSettledDraws(ctx context.Context, until time.Time, limit int) ([]ports.RewardFeedRow, error) {
	if until.IsZero() || limit < 1 || limit > 100 {
		return nil, errors.New("invalid reward feed query")
	}
	rows := make([]ports.RewardFeedRow, 0)
	err := r.db.WithContext(ctx).Table("pulse_reward_grant").
		Select("reward_type, amount, created_at").
		Where("trigger_type = ? AND status = ? AND reversed_at IS NULL AND created_at <= ?", "pulse", "settled", until).
		Where("reward_type IN ? AND amount > 0 AND amount <= ?", []string{"newapi_quota", "community_exp"}, int64((1<<53)-1)).
		Order("created_at DESC, id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}

func (r *rewardFeedRepository) ListSettledDrawsForAdmin(ctx context.Context, until time.Time, limit int) ([]ports.AdminRewardFeedRow, error) {
	if until.IsZero() || limit < 1 || limit > 100 {
		return nil, errors.New("invalid reward feed query")
	}
	rows := make([]ports.AdminRewardFeedRow, 0)
	err := r.db.WithContext(ctx).Table("pulse_reward_grant").
		Select("user_id, reward_type, amount, created_at").
		Where("trigger_type = ? AND status = ? AND reversed_at IS NULL AND created_at <= ?", "pulse", "settled", until).
		Where("reward_type IN ? AND amount > 0 AND amount <= ?", []string{"newapi_quota", "community_exp"}, int64((1<<53)-1)).
		Order("created_at DESC, id DESC").Limit(limit).Scan(&rows).Error
	return rows, err
}
