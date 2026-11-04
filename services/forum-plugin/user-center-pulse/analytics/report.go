package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type Ratio struct {
	Numerator   int `json:"numerator"`
	Denominator int `json:"denominator"`
}
type Daily struct {
	Date       string `json:"date"`
	Registered int    `json:"registered"`
	Visitors   int    `json:"visitors"`
	Active     int    `json:"active"`
	Effective  int    `json:"effective"`
	Authors    int    `json:"authors"`
	Answerers  int    `json:"answerers"`
	Posts      int    `json:"posts"`
	Answers    int    `json:"answers"`
	Observed   bool   `json:"observed"`
}
type Cohort struct {
	Date                string `json:"date"`
	Registered          int    `json:"registered"`
	D1                  *Ratio `json:"d1"`
	D7                  *Ratio `json:"d7"`
	D30                 *Ratio `json:"d30"`
	WorkdaysKnown       bool   `json:"workdays_known"`
	WorkdaysMature      bool   `json:"workdays_mature"`
	WorkdayUsers        int    `json:"workday_users"`
	WorkdayActiveDays   int    `json:"workday_active_days"`
	WorkdayDistribution [8]int `json:"workday_distribution"`
}
type Summary struct {
	Registered       int    `json:"registered"`
	Available        int    `json:"available"`
	Activated        int    `json:"activated"`
	New7             int    `json:"new7"`
	DAU              *int   `json:"dau"`
	WAU              *int   `json:"wau"`
	MAU              *int   `json:"mau"`
	EffectiveDAU     *int   `json:"effective_dau"`
	EffectiveWAU     *int   `json:"effective_wau"`
	EffectiveMAU     *int   `json:"effective_mau"`
	Authors7         int    `json:"authors7"`
	Answerers7       int    `json:"answerers7"`
	Posts7           int    `json:"posts7"`
	Answers7         int    `json:"answers7"`
	Unanswered       int    `json:"unanswered"`
	Reply24h         Ratio  `json:"reply24h"`
	FirstReplyMedian *int64 `json:"first_reply_median_seconds"`
}
type Conversion struct {
	WindowDays int    `json:"window_days"`
	Rate       Ratio  `json:"rate"`
	Matched    int    `json:"matched"`
	Registered int    `json:"registered"`
	Median     *int64 `json:"median_seconds"`
	P75        *int64 `json:"p75_seconds"`
}
type Report struct {
	AsOf          string     `json:"as_of"`
	StartedAt     int64      `json:"started_at"`
	FirstFullDay  string     `json:"first_full_day"`
	Summary       Summary    `json:"summary"`
	Daily         []Daily    `json:"daily"`
	Cohorts       []Cohort   `json:"cohorts"`
	Conversion    Conversion `json:"conversion"`
	CalendarYears []int      `json:"calendar_years"`
}
type registration struct {
	ID string
	At int64
}
type activity struct{ User, Day, Kind string }

func eligible(alias string) string {
	return "NOT EXISTS(SELECT 1 FROM user_role_rel r WHERE r.user_id=" + alias + ".id AND r.role_id=2) AND NOT EXISTS(SELECT 1 FROM metar_ops_label l WHERE l.user_id=" + alias + ".id AND l.label='内部测试')"
}
func quantile(v []int64, p int) *int64 {
	if len(v) == 0 {
		return nil
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	i := (len(v)*p+99)/100 - 1
	x := v[i]
	return &x
}
func (s *Store) Report(ctx context.Context, admin string, days int) (Report, error) {
	out := Report{Daily: []Daily{}, Cohorts: []Cohort{}, CalendarYears: []int{2026}, Conversion: Conversion{WindowDays: 7}}
	if e := s.CheckAdmin(ctx, admin); e != nil {
		return out, e
	}
	if days != 7 && days != 30 && days != 90 {
		return out, ErrInvalid
	}
	today := StartDay(s.Now())
	start := today.AddDate(0, 0, -days)
	lookback := start
	if days < 30 {
		lookback = today.AddDate(0, 0, -30)
	}
	out.AsOf = Day(today.AddDate(0, 0, -1))
	if e := s.DB.QueryRowContext(ctx, "SELECT started_at FROM metar_ops_config WHERE id=1").Scan(&out.StartedAt); e != nil {
		return out, e
	}
	first := StartDay(time.Unix(out.StartedAt, 0))
	if time.Unix(out.StartedAt, 0).After(first) {
		first = first.AddDate(0, 0, 1)
	}
	out.FirstFullDay = Day(first)
	q := "SELECT COUNT(*),COALESCE(SUM(u.status=1),0),COALESCE(SUM(u.status=1 AND u.mail_status=1),0) FROM `user` u WHERE " + eligible("u")
	if e := s.DB.QueryRowContext(ctx, q).Scan(&out.Summary.Registered, &out.Summary.Available, &out.Summary.Activated); e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT u.id,UNIX_TIMESTAMP(u.created_at) FROM `user` u WHERE u.created_at>=FROM_UNIXTIME(?) AND u.created_at<FROM_UNIXTIME(?) AND "+eligible("u")+" ORDER BY u.id LIMIT 50001", lookback.Unix(), today.Unix())
	if e != nil {
		return out, e
	}
	users := []registration{}
	for rows.Next() {
		var u registration
		if e = rows.Scan(&u.ID, &u.At); e != nil {
			rows.Close()
			return out, e
		}
		users = append(users, u)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(users) > 50000 {
		return out, ErrLimit
	}
	acts := []activity{}
	// Content facts are read from Answer; current moderation state determines validity.
	queries := []string{
		"SELECT d.user_id,d.day,IF(d.effective,'read','visit') FROM metar_ops_day d JOIN `user` u ON u.id=d.user_id WHERE d.day>=? AND d.day<? AND " + eligible("u"),
		"SELECT q.user_id,DATE_FORMAT(CONVERT_TZ(q.created_at,'+00:00','+08:00'),'%Y-%m-%d'),'question' FROM question q JOIN `user` u ON u.id=q.user_id WHERE q.status IN(1,2) AND q.created_at>=FROM_UNIXTIME(?) AND q.created_at<FROM_UNIXTIME(?) AND " + eligible("u"),
		"SELECT a.user_id,DATE_FORMAT(CONVERT_TZ(a.created_at,'+00:00','+08:00'),'%Y-%m-%d'),'answer' FROM answer a JOIN question q ON q.id=a.question_id JOIN `user` u ON u.id=a.user_id WHERE a.status=1 AND q.status IN(1,2) AND a.created_at>=FROM_UNIXTIME(?) AND a.created_at<FROM_UNIXTIME(?) AND " + eligible("u"),
	}
	for i, query := range queries {
		var args []any
		if i == 0 {
			args = []any{Day(lookback), Day(today)}
		} else {
			args = []any{lookback.Unix(), today.Unix()}
		}
		rows, e = s.DB.QueryContext(ctx, query+" LIMIT 200001", args...)
		if e != nil {
			return out, e
		}
		n := 0
		for rows.Next() {
			var a activity
			if e = rows.Scan(&a.User, &a.Day, &a.Kind); e != nil {
				rows.Close()
				return out, e
			}
			acts = append(acts, a)
			n++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		if n > 200000 {
			return out, ErrLimit
		}
	}
	out.Daily, out.Cohorts = aggregate(users, acts, start, today, first)
	active, effective := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, a := range acts {
		if active[a.Day] == nil {
			active[a.Day] = map[string]bool{}
		}
		active[a.Day][a.User] = true
		if a.Kind != "visit" {
			if effective[a.Day] == nil {
				effective[a.Day] = map[string]bool{}
			}
			effective[a.Day][a.User] = true
		}
	}
	distinct := func(m map[string]map[string]bool, n int) *int {
		if today.AddDate(0, 0, -n).Before(first) {
			return nil
		}
		set := map[string]bool{}
		for d := today.AddDate(0, 0, -n); d.Before(today); d = d.AddDate(0, 0, 1) {
			for id := range m[Day(d)] {
				set[id] = true
			}
		}
		v := len(set)
		return &v
	}
	out.Summary.DAU = distinct(active, 1)
	out.Summary.WAU = distinct(active, 7)
	out.Summary.MAU = distinct(active, 30)
	out.Summary.EffectiveDAU = distinct(effective, 1)
	out.Summary.EffectiveWAU = distinct(effective, 7)
	out.Summary.EffectiveMAU = distinct(effective, 30)
	authors, answerers := map[string]bool{}, map[string]bool{}
	week := Day(today.AddDate(0, 0, -7))
	for _, a := range acts {
		if a.Day < week {
			continue
		}
		if a.Kind == "question" {
			authors[a.User] = true
			out.Summary.Posts7++
		}
		if a.Kind == "answer" {
			answerers[a.User] = true
			out.Summary.Answers7++
		}
	}
	out.Summary.Authors7 = len(authors)
	out.Summary.Answerers7 = len(answerers)
	for _, u := range users {
		if Day(time.Unix(u.At, 0)) >= week {
			out.Summary.New7++
		}
	}
	rows, e = s.DB.QueryContext(ctx, "SELECT day,COUNT(*) FROM metar_ops_day WHERE user_id IS NULL AND day>=? AND day<? GROUP BY day", Day(start), Day(today))
	if e != nil {
		return out, e
	}
	visits := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		if e = rows.Scan(&d, &n); e != nil {
			rows.Close()
			return out, e
		}
		visits[d] = n
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Daily {
		out.Daily[i].Visitors = visits[out.Daily[i].Date]
	}
	if e = s.contentHealth(ctx, &out, today); e != nil {
		return out, e
	}
	if e = s.conversion(ctx, &out, start, today); e != nil {
		return out, e
	}
	return out, nil
}
func aggregate(users []registration, acts []activity, start, today, first time.Time) ([]Daily, []Cohort) {
	active, effective, authors, answerers := map[string]map[string]bool{}, map[string]map[string]bool{}, map[string]map[string]bool{}, map[string]map[string]bool{}
	posts, answers, newusers := map[string]int{}, map[string]int{}, map[string][]registration{}
	add := func(m map[string]map[string]bool, d, id string) {
		if m[d] == nil {
			m[d] = map[string]bool{}
		}
		m[d][id] = true
	}
	for _, a := range acts {
		add(active, a.Day, a.User)
		if a.Kind != "visit" {
			add(effective, a.Day, a.User)
		}
		if a.Kind == "question" {
			posts[a.Day]++
			add(authors, a.Day, a.User)
		}
		if a.Kind == "answer" {
			answers[a.Day]++
			add(answerers, a.Day, a.User)
		}
	}
	for _, u := range users {
		d := Day(time.Unix(u.At, 0))
		newusers[d] = append(newusers[d], u)
	}
	daily, cohorts := []Daily{}, []Cohort{}
	for d := start; d.Before(today); d = d.AddDate(0, 0, 1) {
		key := Day(d)
		members := newusers[key]
		daily = append(daily, Daily{Date: key, Registered: len(members), Active: len(active[key]), Effective: len(effective[key]), Authors: len(authors[key]), Answerers: len(answerers[key]), Posts: posts[key], Answers: answers[key], Observed: !d.Before(first)})
		c := Cohort{Date: key, Registered: len(members)}
		retention := func(n int) *Ratio {
			target := d.AddDate(0, 0, n)
			if d.Before(first) || !target.Before(today) {
				return nil
			}
			r := Ratio{Denominator: len(members)}
			for _, u := range members {
				if effective[Day(target)][u.ID] {
					r.Numerator++
				}
			}
			return &r
		}
		c.D1 = retention(1)
		c.D7 = retention(7)
		c.D30 = retention(30)
		workdays, known := FirstWorkdays(d)
		c.WorkdaysKnown = known
		c.WorkdaysMature = known && !d.Before(first) && workdays[6] < Day(today)
		if c.WorkdaysMature {
			c.WorkdayUsers = len(members)
			for _, u := range members {
				n := 0
				for _, day := range workdays {
					if effective[day][u.ID] {
						n++
					}
				}
				c.WorkdayActiveDays += n
				c.WorkdayDistribution[n]++
			}
		}
		cohorts = append(cohorts, c)
	}
	return daily, cohorts
}
func (s *Store) contentHealth(ctx context.Context, out *Report, today time.Time) error {
	q := "SELECT COUNT(*) FROM question q JOIN `user` u ON u.id=q.user_id WHERE q.status IN(1,2) AND " + eligible("u") + " AND NOT EXISTS(SELECT 1 FROM answer a WHERE a.question_id=q.id AND a.status=1 AND a.user_id<>q.user_id)"
	if e := s.DB.QueryRowContext(ctx, q).Scan(&out.Summary.Unanswered); e != nil {
		return e
	}
	// Use a seven-day publication cohort with a full 24-hour response opportunity.
	rows, e := s.DB.QueryContext(ctx, "SELECT UNIX_TIMESTAMP(q.created_at),MIN(UNIX_TIMESTAMP(a.created_at)) FROM question q JOIN `user` u ON u.id=q.user_id LEFT JOIN answer a ON a.question_id=q.id AND a.status=1 AND a.user_id<>q.user_id AND a.created_at>=q.created_at AND a.created_at<FROM_UNIXTIME(?) WHERE q.status IN(1,2) AND q.created_at>=FROM_UNIXTIME(?) AND q.created_at<FROM_UNIXTIME(?) AND "+eligible("u")+" GROUP BY q.id,q.created_at LIMIT 50001", today.Unix(), today.AddDate(0, 0, -8).Unix(), today.AddDate(0, 0, -1).Unix())
	if e != nil {
		return e
	}
	defer rows.Close()
	delays := []int64{}
	for rows.Next() {
		var created int64
		var reply *int64
		if e = rows.Scan(&created, &reply); e != nil {
			return e
		}
		out.Summary.Reply24h.Denominator++
		if reply != nil {
			delta := *reply - created
			delays = append(delays, delta)
			if delta <= 86400 {
				out.Summary.Reply24h.Numerator++
			}
		}
	}
	if out.Summary.Reply24h.Denominator > 50000 {
		return ErrLimit
	}
	out.Summary.FirstReplyMedian = quantile(delays, 50)
	return rows.Err()
}
func (s *Store) conversion(ctx context.Context, out *Report, start, today time.Time) error {
	// Fixed seven-day cohort conversion, excluding cohorts that cannot yet mature.
	lower := start.Unix()
	if lower < out.StartedAt {
		lower = out.StartedAt
	}
	upper := today.AddDate(0, 0, -7).Unix()
	query := `SELECT COUNT(*),COALESCE(SUM(registered_at IS NOT NULL AND registered_at-first_seen<=604800),0) FROM metar_ops_visitor v WHERE existing_user=FALSE AND first_seen>=? AND first_seen<? AND (converted_user IS NULL OR EXISTS(SELECT 1 FROM ` + "`user`" + ` u WHERE u.id=v.converted_user AND ` + eligible("u") + `))`
	if upper > lower {
		if e := s.DB.QueryRowContext(ctx, query, lower, upper).Scan(&out.Conversion.Rate.Denominator, &out.Conversion.Rate.Numerator); e != nil {
			return e
		}
	}
	if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user` u WHERE created_at>=FROM_UNIXTIME(?) AND created_at<FROM_UNIXTIME(?) AND "+eligible("u"), lower, today.Unix()).Scan(&out.Conversion.Registered); e != nil {
		return e
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT v.registered_at-v.first_seen FROM metar_ops_visitor v JOIN `user` u ON u.id=v.converted_user WHERE v.registered_at>=? AND v.registered_at<? AND "+eligible("u")+" LIMIT 50001", lower, today.Unix())
	if e != nil {
		return e
	}
	defer rows.Close()
	delays := []int64{}
	for rows.Next() {
		var d int64
		if e = rows.Scan(&d); e != nil {
			return e
		}
		delays = append(delays, d)
	}
	if len(delays) > 50000 {
		return fmt.Errorf("%w: conversion", ErrLimit)
	}
	out.Conversion.Matched = len(delays)
	out.Conversion.Median = quantile(delays, 50)
	out.Conversion.P75 = quantile(delays, 75)
	return rows.Err()
}
