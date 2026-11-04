package analytics

import (
	"reflect"
	"testing"
	"time"
)

func date(v string) time.Time {
	t, e := time.ParseInLocation("2006-01-02 15:04:05", v, Shanghai)
	if e != nil {
		panic(e)
	}
	return t
}
func TestStatutoryWorkdays(t *testing.T) {
	for d, want := range map[string]bool{"2026-01-02": false, "2026-01-04": true, "2026-02-14": true, "2026-02-23": false, "2026-02-28": true, "2026-05-09": true, "2026-09-20": true, "2026-10-07": false, "2026-10-10": true, "2026-10-11": false} {
		got, known := Workday(date(d + " 00:00:00"))
		if !known || got != want {
			t.Fatal(d, got, known)
		}
	}
	got, known := FirstWorkdays(date("2026-09-30 23:59:59"))
	want := []string{"2026-10-08", "2026-10-09", "2026-10-10", "2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15"}
	if !known || !reflect.DeepEqual(got, want) {
		t.Fatal(got, known)
	}
	if _, known = FirstWorkdays(date("2026-12-30 00:00:00")); known {
		t.Fatal("unpublished next-year calendar must not guess")
	}
	if Day(time.Date(2026, 10, 1, 16, 0, 0, 0, time.UTC)) != "2026-10-02" {
		t.Fatal("Shanghai midnight")
	}
}
func TestCohortMaturityDedupAndExactDay(t *testing.T) {
	start := date("2026-09-30 00:00:00")
	users := []registration{{"2", start.Unix()}, {"3", start.Unix()}}
	acts := []activity{{"2", "2026-10-07", "read"}, {"2", "2026-10-07", "question"}, {"3", "2026-10-06", "read"}, {"3", "2026-10-07", "visit"}, {"2", "2026-10-10", "read"}, {"2", "2026-10-10", "answer"}}
	daily, c := aggregate(users, acts, start, date("2026-10-16 00:00:00"), start)
	if *c[0].D7 != (Ratio{1, 2}) || c[0].D30 != nil || c[0].WorkdayActiveDays != 1 || c[0].WorkdayDistribution[0] != 1 || c[0].WorkdayDistribution[1] != 1 {
		t.Fatalf("bad cohort %+v", c[0])
	}
	if daily[7].Active != 2 || daily[7].Effective != 1 {
		t.Fatal(daily[7])
	}
	_, c = aggregate(users, acts, start, date("2026-10-07 00:00:00"), start)
	if c[0].D7 != nil || c[0].WorkdaysMature {
		t.Fatal("immature cohort")
	}
	_, c = aggregate(users, acts, start, date("2026-10-16 00:00:00"), start.AddDate(0, 0, 1))
	if c[0].D1 != nil || c[0].D7 != nil || c[0].WorkdaysMature {
		t.Fatal("missing historic observations")
	}
}
