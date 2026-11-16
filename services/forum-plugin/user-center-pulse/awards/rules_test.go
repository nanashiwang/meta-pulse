package awards

import (
	"testing"
	"time"
)

func date(t *testing.T, value string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04:05", value, Zone)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestBeijingWeekAndNominationDeadline(t *testing.T) {
	sunday := date(t, "2026-11-15 23:59:59")
	if Week(sunday).Format("2006-01-02") != "2026-11-09" {
		t.Fatal(Week(sunday))
	}
	if !CanNominate(sunday, date(t, "2026-11-17 23:59:59")) || CanNominate(sunday, date(t, "2026-11-18 00:00:00")) {
		t.Fatal("Tuesday cutoff")
	}
	if CanNominate(sunday, sunday.Add(-time.Second)) {
		t.Fatal("future post")
	}
	if Week(time.Date(2026, 11, 15, 16, 0, 0, 0, time.UTC)).Format("2006-01-02") != "2026-11-16" {
		t.Fatal("UTC rollover")
	}
}
func TestCrossMonthReviewWaitsForLastMondayWeek(t *testing.T) {
	p := Weekly(date(t, "2026-12-06 23:59:59"), "response")
	if p.Key != "2026-11-30" || p.Key[:7] != "2026-11" {
		t.Fatal(p)
	}
	month := Period{"month", "2026-11", "response"}
	at, _ := month.ReviewAt()
	if at.Format("2006-01-02") != "2026-12-09" {
		t.Fatal(at)
	}
	weeks, _ := MonthWeeks(month)
	if len(weeks) != 5 || weeks[4] != "2026-11-30" {
		t.Fatal(weeks)
	}
	if month.Limit() != 5 || month.Amount() != 100 || p.Limit() != 10 || p.Amount() != 50 {
		t.Fatal("limits")
	}
}
func TestReviewRequiresRealPeriodsReasonsAndBothCriteria(t *testing.T) {
	for _, p := range []Period{{"week", "2026-11-17", "post"}, {"month", "2026-13", "post"}, {"year", "2026", "response"}, {"week", "2026-11-16", "unknown"}} {
		if _, err := p.Start(); err == nil {
			t.Fatal(p)
		}
	}
	c := Command{Op: "select", Period: Period{"week", "2026-11-09", "post"}, SubjectID: "100", Reason: "实际验证", CriterionOne: true, CriterionTwo: true}
	if c.Validate() != nil {
		t.Fatal(c)
	}
	c.CriterionTwo = false
	if c.Validate() == nil {
		t.Fatal("missing criterion")
	}
	c.Op = "reject"
	c.Reason = " "
	if c.Validate() == nil {
		t.Fatal("missing reason")
	}
}

func TestManualPaidAndTitleValidation(t *testing.T) {
	if (Command{Op: "title", TitleKey: "post-month"}).Validate() != nil {
		t.Fatal("valid title")
	}
	if (Command{Op: "title", TitleKey: "arbitrary-admin"}).Validate() == nil {
		t.Fatal("invalid title")
	}
	if (Command{Op: "title"}).Validate() != nil {
		t.Fatal("hide title")
	}
	cmd := Command{Op: "paid", Period: Period{"week", "2026-11-09", "post"}, SubjectID: "100"}
	if cmd.Validate() == nil {
		t.Fatal("payment note required")
	}
	cmd.Reason = "人工转账凭据"
	if cmd.Validate() != nil {
		t.Fatal("valid paid record")
	}
}
