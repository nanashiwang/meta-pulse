// Package analytics owns community operations projections, never reward facts.
package analytics

import "time"

var Shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

// State Council notice 国办发明电〔2025〕7号. Unknown years deliberately fail closed.
// https://www.gov.cn/zhengce/content/202511/content_7047090.htm
var holidays2026 = [][2]string{{"01-01", "01-03"}, {"02-15", "02-23"}, {"04-04", "04-06"}, {"05-01", "05-05"}, {"06-19", "06-21"}, {"09-25", "09-27"}, {"10-01", "10-07"}}
var working2026 = map[string]bool{"01-04": true, "02-14": true, "02-28": true, "05-09": true, "09-20": true, "10-10": true}

func Day(t time.Time) string { return t.In(Shanghai).Format("2006-01-02") }
func StartDay(t time.Time) time.Time {
	t = t.In(Shanghai)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Shanghai)
}
func Workday(t time.Time) (bool, bool) {
	t = t.In(Shanghai)
	if t.Year() != 2026 {
		return false, false
	}
	md := t.Format("01-02")
	if working2026[md] {
		return true, true
	}
	for _, r := range holidays2026 {
		if md >= r[0] && md <= r[1] {
			return false, true
		}
	}
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday, true
}

// Registration day is day zero and never part of the seven workdays.
func FirstWorkdays(registered time.Time) ([]string, bool) {
	out := make([]string, 0, 7)
	for d := StartDay(registered).AddDate(0, 0, 1); len(out) < 7; d = d.AddDate(0, 0, 1) {
		work, known := Workday(d)
		if !known {
			return nil, false
		}
		if work {
			out = append(out, Day(d))
		}
	}
	return out, true
}
