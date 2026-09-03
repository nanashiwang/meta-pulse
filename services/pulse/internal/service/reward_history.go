package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const (
	DefaultRewardHistoryLimit = 20
	MaxRewardHistoryLimit     = 100
)

// RewardHistoryItem is a user-safe projection. Internal budget, source_ref,
// random value, and audit fields never cross the product read endpoint.
type RewardHistoryItem struct {
	GrantID    string    `json:"grant_id"`
	PeriodID   uint64    `json:"period_id"`
	ActionID   string    `json:"action_id"`
	RewardType string    `json:"reward_type"`
	Amount     int64     `json:"amount"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type RewardHistoryService struct {
	unit ports.UnitOfWork
}

func NewRewardHistoryService(unit ports.UnitOfWork) (*RewardHistoryService, error) {
	if unit == nil {
		return nil, errors.New("reward history unit of work is nil")
	}
	return &RewardHistoryService{unit: unit}, nil
}

func (s *RewardHistoryService) List(ctx context.Context, userID uint64, limit int) ([]RewardHistoryItem, error) {
	if userID == 0 {
		return nil, errors.New("invalid reward history user id")
	}
	if limit == 0 {
		limit = DefaultRewardHistoryLimit
	}
	if limit < 1 || limit > MaxRewardHistoryLimit {
		return nil, fmt.Errorf("reward history limit must be between 1 and %d", MaxRewardHistoryLimit)
	}
	var grants []ports.RewardGrant
	if err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.RewardHistory == nil {
			return errors.New("reward history repository is not initialized")
		}
		var err error
		grants, err = repos.RewardHistory.ListGrantsForUser(ctx, userID, limit)
		return err
	}); err != nil {
		return nil, err
	}
	result := make([]RewardHistoryItem, 0, len(grants))
	for _, grant := range grants {
		if strings.TrimSpace(grant.GrantID) == "" || grant.UserID != userID {
			return nil, errors.New("reward history contains invalid grant")
		}
		result = append(result, RewardHistoryItem{GrantID: grant.GrantID, PeriodID: grant.PeriodID, ActionID: grant.ActionID, RewardType: grant.RewardType, Amount: grant.Amount, Status: grant.Status, CreatedAt: grant.CreatedAt})
	}
	return result, nil
}
