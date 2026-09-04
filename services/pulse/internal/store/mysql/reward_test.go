package mysql

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestRewardGrantTransitionRejectsInvalidStateChanges(t *testing.T) {
	repo := &rewardRepository{}
	now := time.Now()
	cases := []struct {
		grantID uint64
		from    string
		to      string
		at      time.Time
	}{
		{0, "pending", "settled", now},
		{1, "pending", "settled", time.Time{}},
		{1, "pending", "reversed", now},
		{1, "settled", "pending", now},
		{1, "reversed", "settled", now},
	}
	for _, test := range cases {
		if err := repo.TransitionGrantStatus(context.Background(), test.grantID, test.from, test.to, test.at); err == nil {
			t.Fatalf("transition id=%d %s -> %s at=%s was accepted", test.grantID, test.from, test.to, test.at)
		}
	}
}

func TestSettlementClaimRejectsInvalidLeaseAndAttemptOverflow(t *testing.T) {
	repo := &rewardRepository{}
	now := time.Now()
	for _, leaseUntil := range []time.Time{time.Time{}, now, now.Add(-time.Second)} {
		if _, err := repo.ClaimDue(context.Background(), now, 1, leaseUntil); err == nil {
			t.Fatalf("lease ending at %s was accepted", leaseUntil)
		}
	}
	if _, err := repo.ClaimDue(context.Background(), time.Time{}, 1, now); err == nil {
		t.Fatal("zero claim time was accepted")
	}
	if next, err := nextSettlementAttempt(math.MaxUint32); err == nil || next != 0 {
		t.Fatalf("overflow attempt next=%d error=%v", next, err)
	}
	if next, err := nextSettlementAttempt(41); err != nil || next != 42 {
		t.Fatalf("normal attempt next=%d error=%v", next, err)
	}
}
