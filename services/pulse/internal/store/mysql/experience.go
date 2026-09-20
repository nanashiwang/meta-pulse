package mysql

import (
	"context"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"gorm.io/gorm/clause"
	"time"
)

func (r *rewardRepository) ListPendingExperience(ctx context.Context, user uint64, limit int) ([]ports.RewardGrant, error) {
	if user == 0 || limit < 1 || limit > 100 {
		return nil, ports.ErrConflict
	}
	var rows []rewardGrantModel
	err := r.db.WithContext(ctx).Table("pulse_reward_grant g").Select("g.*").Joins("JOIN pulse_settlement_outbox o ON o.reward_grant_id=g.id").Where("g.user_id=? AND g.reward_type='community_exp' AND g.status IN ('pending','reversed') AND o.status='community_pending'", user).Order("o.id ASC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ports.RewardGrant, 0, len(rows))
	for _, row := range rows {
		out = append(out, rewardGrantFromModel(row))
	}
	return out, nil
}
func (r *rewardRepository) TransitionExperienceDelivery(ctx context.Context, grant uint64, from, to string, at time.Time) error {
	if grant == 0 || at.IsZero() || (to != "community_pending" && to != "community_delivered") || (from != "community_pending" && from != "community_delivered" && from != "shadow") {
		return ports.ErrConflict
	}
	var completed *time.Time
	if to == "community_delivered" {
		completed = &at
	}
	result := r.db.WithContext(ctx).Table("pulse_settlement_outbox").Where("reward_grant_id=? AND status=? AND reward_grant_id IN (SELECT id FROM pulse_reward_grant WHERE reward_type='community_exp')", grant, from).Updates(map[string]any{"status": to, "completed_at": completed})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ports.ErrConflict
	}
	return nil
}

// A locking read observes the current outbox after waiting for the grant lock,
// even when an earlier lookup established a REPEATABLE READ snapshot.
func (r *rewardRepository) ExperienceDeliveryStatusForUpdate(ctx context.Context, grant uint64) (string, error) {
	var row settlementOutboxModel
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("reward_grant_id = ?", grant).Take(&row).Error
	return row.Status, err
}
