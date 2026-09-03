package service

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type rewardHistoryUnit struct{ store *memoryRewardStore }

func (u rewardHistoryUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{RewardHistory: u.store})
}

func (m *memoryRewardStore) ListGrantsForUser(_ context.Context, userID uint64, limit int) ([]ports.RewardGrant, error) {
	result := make([]ports.RewardGrant, 0, limit)
	for i := len(m.grants) - 1; i >= 0 && len(result) < limit; i-- {
		if m.grants[i].UserID == userID {
			result = append(result, m.grants[i])
		}
	}
	return result, nil
}

func TestRewardHistoryOnlyReturnsUserSafeProjection(t *testing.T) {
	rewards := newMemoryRewardStore()
	rewards.grants = []ports.RewardGrant{{ID: 1, GrantID: "grant-1", PeriodID: 4, UserID: 7, ActionID: "action-1", RewardType: "quota", Amount: 10, Status: GrantStatusSettled, SourceRef: "must-not-leak", RandomValue: "must-not-leak", BudgetType: "loyalty", CreatedAt: time.Unix(1700000000, 0).UTC()}, {ID: 2, GrantID: "grant-2", PeriodID: 4, UserID: 8, ActionID: "other", Amount: 99}}
	history, err := NewRewardHistoryService(rewardHistoryUnit{store: rewards})
	if err != nil {
		t.Fatal(err)
	}
	items, err := history.List(context.Background(), 7, 0)
	if err != nil || len(items) != 1 || items[0].GrantID != "grant-1" || items[0].Amount != 10 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestRewardHistoryRejectsInvalidLimit(t *testing.T) {
	history, err := NewRewardHistoryService(rewardHistoryUnit{store: newMemoryRewardStore()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := history.List(context.Background(), 7, MaxRewardHistoryLimit+1); err == nil {
		t.Fatal("invalid limit accepted")
	}
}
