// Package awards owns content selection only. It never calls Pulse or credits balances.
package awards

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid_request")
var ErrForbidden = errors.New("forbidden")
var ErrClosed = errors.New("period_closed")
var ErrQuota = errors.New("nomination_limit")
var ErrRecommendationLimit = errors.New("recommendation_limit")
var ErrConflict = errors.New("conflict")
var ErrNotFound = errors.New("not_found")
var ErrNotReady = errors.New("review_not_ready")
var idRE = regexp.MustCompile("^[1-9][0-9]{0,19}$")
var keyRE = regexp.MustCompile("^[a-zA-Z0-9_-]{16,80}$")
var Zone = time.FixedZone("Asia/Shanghai", 8*3600)

type Period struct {
	Type string `json:"type"`
	Key  string `json:"key"`
	Kind string `json:"kind"`
}

func Week(t time.Time) time.Time {
	v := t.In(Zone)
	v = time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, Zone)
	return v.AddDate(0, 0, -(int(v.Weekday())+6)%7)
}
func Weekly(t time.Time, kind string) Period {
	return Period{"week", Week(t).Format("2006-01-02"), kind}
}
func (p Period) Start() (time.Time, error) {
	if p.Kind != "post" && p.Kind != "response" {
		return time.Time{}, ErrInvalid
	}
	format := "2006-01-02"
	if p.Type == "month" {
		format = "2006-01"
	} else if p.Type != "week" {
		return time.Time{}, ErrInvalid
	}
	v, err := time.ParseInLocation(format, p.Key, Zone)
	if err != nil || v.Format(format) != p.Key || (p.Type == "week" && v.Weekday() != time.Monday) {
		return v, ErrInvalid
	}
	return v, nil
}
func (p Period) ReviewAt() (time.Time, error) {
	v, err := p.Start()
	if err != nil {
		return v, err
	}
	if p.Type == "week" {
		return v.AddDate(0, 0, 9), nil
	}
	last := v.AddDate(0, 1, -1)
	return Week(last).AddDate(0, 0, 9), nil
}
func (p Period) Limit() int {
	if p.Type == "month" {
		return 5
	}
	return 10
}
func (p Period) Amount() int {
	if p.Type == "month" {
		return 100
	}
	return 50
}
func CanNominate(created, now time.Time) bool {
	return !created.After(now) && now.Before(Week(created).AddDate(0, 0, 9))
}
func MonthWeeks(p Period) ([]string, error) {
	v, err := p.Start()
	if err != nil || p.Type != "month" {
		return nil, ErrInvalid
	}
	out := []string{}
	for d := v; d.Month() == v.Month(); d = d.AddDate(0, 0, 1) {
		if d.Weekday() == time.Monday {
			out = append(out, d.Format("2006-01-02"))
		}
	}
	return out, nil
}

type Command struct {
	TitleKey      string `json:"title_key"`
	Op            string `json:"op"`
	Period        Period `json:"period"`
	SubjectID     string `json:"subject_id"`
	QuestionID    string `json:"question_id"`
	Reason        string `json:"reason"`
	RecommenderID string `json:"recommender_id,omitempty"`
	CriterionOne  bool   `json:"criterion_one"`
	CriterionTwo  bool   `json:"criterion_two"`
	Version       int64  `json:"version"`
}

func (c Command) Validate() error {
	if c.Op == "title" {
		if !validTitle(c.TitleKey) {
			return ErrInvalid
		}
		return nil
	}
	if c.Op == "nominate" {
		if !idRE.MatchString(c.QuestionID) {
			return ErrInvalid
		}
		return nil
	}
	if _, err := c.Period.Start(); err != nil {
		return err
	}
	if c.Version < 0 {
		return ErrInvalid
	}
	switch c.Op {
	case "withdraw":
		if c.Period.Type != "week" || !idRE.MatchString(c.SubjectID) {
			return ErrInvalid
		}
		return nil
	case "sync", "publish":
		return nil
	case "rec_paid":
		if c.Period.Type != "week" || c.Period.Kind != "post" || !idRE.MatchString(c.SubjectID) || !idRE.MatchString(c.RecommenderID) || strings.TrimSpace(c.Reason) == "" || len([]rune(c.Reason)) > 2000 {
			return ErrInvalid
		}
		return nil
	case "select", "reject", "revoke", "reconfirm", "paid":
		if !idRE.MatchString(c.SubjectID) || strings.TrimSpace(c.Reason) == "" || len([]rune(c.Reason)) > 2000 {
			return ErrInvalid
		}
		if (c.Op == "select" || c.Op == "reconfirm") && (!c.CriterionOne || !c.CriterionTwo) {
			return ErrInvalid
		}
		return nil
	}
	return ErrInvalid
}
