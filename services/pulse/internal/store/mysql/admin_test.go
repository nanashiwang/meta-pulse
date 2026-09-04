package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

func TestContentBusinessDayBoundsUseAsiaShanghai(t *testing.T) {
	start, end := contentBusinessDayBounds(time.Date(2026, 9, 3, 16, 0, 0, 0, time.UTC))
	if start.Format(time.RFC3339) != "2026-09-04T00:00:00+08:00" || end.Format(time.RFC3339) != "2026-09-05T00:00:00+08:00" {
		t.Fatalf("bounds=%s..%s", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	previous, _ := contentBusinessDayBounds(time.Date(2026, 9, 3, 15, 59, 59, 0, time.UTC))
	if previous.Format(time.RFC3339) != "2026-09-03T00:00:00+08:00" {
		t.Fatalf("previous start=%s", previous.Format(time.RFC3339))
	}
}

func TestContentAwardTransitionOnlyAllowsSettledToReversed(t *testing.T) {
	repo := &contentRepository{}
	cases := []struct {
		actionID string
		from     string
		to       string
	}{
		{"", ports.ContentAwardSettled, ports.ContentAwardReversed},
		{"award", ports.ContentAwardPending, ports.ContentAwardReversed},
		{"award", ports.ContentAwardSettled, ports.ContentAwardPending},
		{"award", ports.ContentAwardReversed, ports.ContentAwardSettled},
	}
	for _, test := range cases {
		if err := repo.TransitionAwardStatus(context.Background(), test.actionID, test.from, test.to); err == nil {
			t.Fatalf("transition %q %s -> %s was accepted", test.actionID, test.from, test.to)
		}
	}
}

func TestParseAggregateInt64RejectsOverflow(t *testing.T) {
	valid := map[string]int64{
		"0":                    0,
		"9223372036854775807":  9223372036854775807,
		"-9223372036854775808": -9223372036854775808,
	}
	for raw, want := range valid {
		got, err := parseAggregateInt64(raw)
		if err != nil || got != want {
			t.Fatalf("raw=%q got=%d err=%v want=%d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "9223372036854775808", "-9223372036854775809", "1.5", "not-a-number"} {
		if _, err := parseAggregateInt64(raw); err == nil {
			t.Fatalf("invalid aggregate %q was accepted", raw)
		}
	}
}
