package mysql

import (
	"context"
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
