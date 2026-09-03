package period

import (
	"errors"
	"testing"
	"time"
)

func TestPeriodUsesHalfOpenInterval(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := Period{ID: 1, Status: StatusActive, StartsAt: start, EndsAt: start.Add(time.Hour)}
	if !p.Contains(start) || p.Contains(p.EndsAt) {
		t.Fatal("period boundary is not half-open")
	}
}

func TestResolveRejectsOverlap(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := ResolveActive([]Period{
		{ID: 1, Status: StatusActive, StartsAt: start, EndsAt: start.Add(2 * time.Hour)},
		{ID: 2, Status: StatusActive, StartsAt: start.Add(time.Hour), EndsAt: start.Add(3 * time.Hour)},
	}, start.Add(90*time.Minute))
	if err == nil || errors.Is(err, ErrNoActivePeriod) {
		t.Fatalf("error = %v, want overlap error", err)
	}
}
