// Package growth owns non-monetary community experience. It never writes
// Answer reputation, Pulse contribution/tickets or new-api balances.
package growth

import (
	"errors"
	"time"
)

var Zone = time.FixedZone("Asia/Shanghai", 8*3600)
var ErrForbidden = errors.New("account_unavailable")
var ErrConflict = errors.New("conflict")
var ErrInvalid = errors.New("invalid_request")

type Level struct {
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Minimum int64  `json:"minimum"`
}

var Levels = []Level{{0, "新朋友", 0}, {1, "初来乍到", 150}, {2, "活跃成员", 600}, {3, "热心交流者", 1750}, {4, "社区熟面孔", 4500}, {5, "资深参与者", 10000}, {6, "社区共建者", 22500}, {7, "长期贡献者", 45000}, {8, "社区同行者", 90000}}

func LevelFor(total int64) Level {
	result := Levels[0]
	for _, l := range Levels {
		if total >= l.Minimum {
			result = l
		}
	}
	return result
}
func Day(t time.Time) string { return t.In(Zone).Format("2006-01-02") }

type Rule struct {
	Amount      int64 `json:"amount"`
	DailyCount  int   `json:"daily_count"`
	DailyAmount int64 `json:"daily_amount"`
}
type Rules struct {
	DailyCap             int64 `json:"daily_cap"`
	Checkin              int64 `json:"checkin"`
	StreakCheckin        int64 `json:"streak_checkin"`
	Question             Rule  `json:"question"`
	Answer               Rule  `json:"answer"`
	Like                 Rule  `json:"like"`
	Accepted             Rule  `json:"accepted"`
	Featured             int64 `json:"featured"`
	FeaturedMonthlyCount int   `json:"featured_monthly_count"`
}

func DefaultRules() Rules {
	return Rules{60, 2, 3, Rule{5, 1, 5}, Rule{8, 2, 16}, Rule{2, 10, 20}, Rule{20, 1, 20}, 50, 4}
}
func (r Rules) Validate() error {
	if r.DailyCap < 1 || r.DailyCap > 1000 || r.Checkin < 1 || r.StreakCheckin < r.Checkin || r.StreakCheckin > r.DailyCap || r.Featured < 1 || r.Featured > 1000 || r.FeaturedMonthlyCount < 1 || r.FeaturedMonthlyCount > 10 {
		return ErrInvalid
	}
	for _, v := range []Rule{r.Question, r.Answer, r.Like, r.Accepted} {
		if v.Amount < 1 || v.Amount > r.DailyCap || v.DailyCount < 1 || v.DailyCount > 100 || v.DailyAmount < v.Amount || v.DailyAmount > r.DailyCap {
			return ErrInvalid
		}
	}
	return nil
}
func (r Rules) For(kind string) (Rule, bool) {
	switch kind {
	case "question":
		return r.Question, true
	case "answer":
		return r.Answer, true
	case "like":
		return r.Like, true
	case "accepted":
		return r.Accepted, true
	}
	return Rule{}, false
}
func CheckinReward(r Rules, last string, streak int, now time.Time) (int64, int) {
	if last == Day(now) {
		return 0, streak
	}
	if last == Day(now.AddDate(0, 0, -1)) {
		streak++
	} else {
		streak = 1
	}
	if streak >= 7 {
		return r.StreakCheckin, streak
	}
	return r.Checkin, streak
}
func AppearanceAllowed(value string, level int) bool {
	switch value {
	case "default":
		return true
	case "frame":
		return level >= 3
	case "sage":
		return level >= 5
	case "builder":
		return level >= 6
	case "veteran":
		return level >= 7
	case "companion":
		return level >= 8
	}
	return false
}
