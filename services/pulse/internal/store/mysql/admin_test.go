package mysql

import (
	"testing"
	"time"
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
