package ports

import (
	"context"
	"time"
)

// RewardFeedRow deliberately has no identity, grant, action or audit fields.
type RewardFeedRow struct {
	RewardType string
	Amount     int64
	CreatedAt  time.Time
}

type RewardFeedRepository interface {
	ListSettledDraws(context.Context, time.Time, time.Time, int) ([]RewardFeedRow, error)
}
