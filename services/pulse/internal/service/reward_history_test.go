package service

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type rewardHistoryUnit struct{ store *memoryRewardStore }

func (u rewardHistoryUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{RewardHistory: u.store, Reward: u.store})
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

func TestRewardHistorySettlementProjectionPreservesGrant(t *testing.T) {
	for _, tc := range []struct{ grant, outbox, kind, want string }{
		{"pending", "dead", "newapi_quota", "settlement_dead"},
		{"pending", "conflict", "newapi_quota", "settlement_dead"},
		{"pending", "retry", "newapi_quota", "pending"},
		{"pending", "processing", "newapi_quota", "pending"},
		{"pending", "", "newapi_quota", "pending"},
		{"pending", "dead", "community_exp", "pending"},
		{"settled", "dead", "newapi_quota", "settled"},
		{"reversed", "dead", "newapi_quota", "reversed"},
	} {
		t.Run(tc.grant+tc.outbox+tc.kind, func(t *testing.T) {
			store := newMemoryRewardStore()
			grant := ports.RewardGrant{ID: 1, GrantID: "g", UserID: 7, ActionID: "action", TriggerType: "pulse", Status: tc.grant, SettlementStatus: tc.outbox, RewardType: tc.kind}
			store.grants = []ports.RewardGrant{grant}
			history, _ := NewRewardHistoryService(rewardHistoryUnit{store: store})
			rows, err := history.List(context.Background(), 7, 20)
			if err != nil || len(rows) != 1 || rows[0].Status != tc.want {
				t.Fatalf("history: %+v %v", rows, err)
			}
			rows, err = history.FindAction(context.Background(), 7, "action")
			if err != nil || len(rows) != 1 || rows[0].Status != tc.want {
				t.Fatalf("action: %+v %v", rows, err)
			}
			if store.grants[0].Status != tc.grant {
				t.Fatal("persisted grant mutated")
			}
			store.grants[0].Status = "settled"
			store.grants[0].SettlementStatus = "completed"
			rows, err = history.List(context.Background(), 7, 20)
			if err != nil || rows[0].Status != "settled" {
				t.Fatal("recovery remains blocked")
			}
		})
	}
}
