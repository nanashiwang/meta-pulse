package ports

import (
	"context"
	"time"
)

// RewardFeedRow is the anonymous public projection.
type RewardFeedRow struct {
	RewardType string
	Amount     int64
	CreatedAt  time.Time
}

// AdminRewardFeedRow is used only behind the signed admin-only endpoint.
type AdminRewardFeedRow struct {
	UserID     uint64
	RewardType string
	Amount     int64
	CreatedAt  time.Time
}

type RewardFeedRepository interface {
	ListSettledDraws(context.Context, time.Time, int) ([]RewardFeedRow, error)
	ListSettledDrawsForAdmin(context.Context, time.Time, int) ([]AdminRewardFeedRow, error)
}
