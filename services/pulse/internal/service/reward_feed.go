package service

import (
	"context"
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const RewardFeedLimit = 20

type RewardFeedItem struct {
	RewardType string    `json:"reward_type"`
	Amount     int64     `json:"amount"`
	WonAt      time.Time `json:"won_at"`
}

type RewardFeed struct {
	Rewards      []RewardFeedItem `json:"rewards"`
	QuotaPerUnit int64            `json:"quota_per_unit"`
}

type RewardFeedService struct {
	unit         ports.UnitOfWork
	quotaPerUnit int64
	now          func() time.Time
}

func NewRewardFeedService(unit ports.UnitOfWork, quotaPerUnit int64, now func() time.Time) (*RewardFeedService, error) {
	if unit == nil || quotaPerUnit < 0 || quotaPerUnit > (1<<53)-1 || now == nil {
		return nil, errors.New("invalid reward feed configuration")
	}
	return &RewardFeedService{unit: unit, quotaPerUnit: quotaPerUnit, now: now}, nil
}

func (s *RewardFeedService) Recent(ctx context.Context) (RewardFeed, error) {
	now := s.now().UTC()
	since := now.Add(-30 * 24 * time.Hour)
	result := RewardFeed{Rewards: make([]RewardFeedItem, 0), QuotaPerUnit: s.quotaPerUnit}
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.RewardFeed == nil {
			return errors.New("reward feed repository unavailable")
		}
		rows, err := repos.RewardFeed.ListSettledDraws(ctx, since, now, RewardFeedLimit)
		if err != nil {
			return err
		}
		if len(rows) > RewardFeedLimit {
			return errors.New("invalid reward feed size")
		}
		for _, row := range rows {
			if (row.RewardType != "newapi_quota" && row.RewardType != "community_exp") || row.Amount <= 0 || row.Amount > (1<<53)-1 || row.CreatedAt.Before(since) || row.CreatedAt.After(now) {
				return errors.New("invalid reward feed row")
			}
			result.Rewards = append(result.Rewards, RewardFeedItem{RewardType: row.RewardType, Amount: row.Amount, WonAt: row.CreatedAt.UTC().Truncate(time.Minute)})
		}
		return nil
	})
	if err != nil {
		return RewardFeed{}, err
	}
	return result, nil
}
