package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type feedUnit struct {
	rows         []ports.RewardFeedRow
	err          error
	since, until time.Time
	limit        int
}

func (u *feedUnit) Do(_ context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{RewardFeed: u})
}
func (u *feedUnit) ListSettledDraws(_ context.Context, since, until time.Time, limit int) ([]ports.RewardFeedRow, error) {
	u.since = since
	u.until = until
	u.limit = limit
	return u.rows, u.err
}
func TestRewardFeedProjectionAndBoundedWindow(t *testing.T) {
	now := time.Date(2026, 10, 15, 10, 20, 49, 987, time.FixedZone("local", 8*3600))
	unit := &feedUnit{rows: []ports.RewardFeedRow{{RewardType: "newapi_quota", Amount: 625000, CreatedAt: now}}}
	service, err := NewRewardFeedService(unit, 500000, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Recent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if unit.limit != 20 || !unit.until.Equal(now) || !unit.since.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatal("unbounded query")
	}
	body, _ := json.Marshal(result)
	if string(body) != `{"rewards":[{"reward_type":"newapi_quota","amount":625000,"won_at":"2026-10-15T02:20:00Z"}],"quota_per_unit":500000}` {
		t.Fatal(string(body))
	}
	for _, field := range []string{"user_id", "grant_id", "action_id", "source_ref", "random_value", "username", "avatar"} {
		if strings.Contains(string(body), field) {
			t.Fatal("private field", field)
		}
	}
	unit.rows = nil
	result, err = service.Recent(context.Background())
	if err != nil || result.Rewards == nil || len(result.Rewards) != 0 {
		t.Fatalf("empty feed: %+v %v", result, err)
	}
}
func TestRewardFeedRejectsMalformedAndFailedReads(t *testing.T) {
	now := time.Now().UTC()
	base := ports.RewardFeedRow{RewardType: "community_exp", Amount: 10, CreatedAt: now}
	cases := []ports.RewardFeedRow{base, base, base, base, base}
	cases[0].RewardType = "cash"
	cases[1].Amount = 0
	cases[2].Amount = 1 << 53
	cases[3].CreatedAt = now.Add(-31 * 24 * time.Hour)
	cases[4].CreatedAt = now.Add(time.Second)
	for _, row := range cases {
		u := &feedUnit{rows: []ports.RewardFeedRow{row}}
		s, _ := NewRewardFeedService(u, 500000, func() time.Time { return now })
		if _, err := s.Recent(context.Background()); err == nil {
			t.Fatal("invalid row accepted")
		}
	}
	u := &feedUnit{rows: make([]ports.RewardFeedRow, 21)}
	s, _ := NewRewardFeedService(u, 500000, func() time.Time { return now })
	if _, err := s.Recent(context.Background()); err == nil {
		t.Fatal("unbounded feed accepted")
	}
	u.err = errors.New("database unavailable")
	if _, err := s.Recent(context.Background()); err == nil {
		t.Fatal("failed read accepted")
	}
}
