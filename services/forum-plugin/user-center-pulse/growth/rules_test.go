package growth

import (
	"testing"
	"time"
)

func TestLevelsFivefold(t *testing.T) {
	values := []int64{0, 150, 600, 1750, 4500, 10000, 22500, 45000, 90000}
	for n, v := range values {
		if Levels[n].Minimum != v || LevelFor(v).Number != n {
			t.Fatalf("level %d threshold", n)
		}
		if n > 0 && LevelFor(v-1).Number != n-1 {
			t.Fatalf("level %d boundary", n)
		}
	}
	if LevelFor(-2).Number != 0 || LevelFor(1000000).Number != 8 {
		t.Fatal("clamping")
	}
}
func TestCheckinCalendarAndStreak(t *testing.T) {
	now := time.Date(2026, 9, 20, 16, 0, 0, 0, time.UTC)
	if Day(now) != "2026-09-21" {
		t.Fatal("timezone")
	}
	r := DefaultRules()
	for _, c := range []struct {
		last   string
		streak int
		amount int64
		next   int
	}{{"2026-09-21", 7, 0, 7}, {"2026-09-20", 5, 2, 6}, {"2026-09-20", 6, 3, 7}, {"2026-09-19", 8, 2, 1}, {"", 0, 2, 1}} {
		a, n := CheckinReward(r, c.last, c.streak, now)
		if a != c.amount || n != c.next {
			t.Fatalf("%+v: %d,%d", c, a, n)
		}
	}
}
func TestRulesAndAppearance(t *testing.T) {
	r := DefaultRules()
	if r.Validate() != nil {
		t.Fatal("defaults")
	}
	r.Like.Amount = 61
	if r.Validate() == nil {
		t.Fatal("invalid cap")
	}
	if AppearanceAllowed("frame", 2) || !AppearanceAllowed("frame", 3) || AppearanceAllowed("admin", 8) {
		t.Fatal("appearance")
	}
}
