package service

import (
	"errors"
	"sort"
	"time"
)

const (
	PageSize       = 15
	MaxPage        = 1000
	newModelWindow = 15 * 24 * time.Hour
)

var ErrBadQuery = errors.New("invalid leaderboard query")

// Query is a validated leaderboard request.
type Query struct {
	// Window limits by release date: "all", "90d" or "30d".
	Window string
	// Sort is one of the column keys below; Desc flips the natural order.
	Sort     string
	Asc      bool
	OpenOnly bool
	Page     int
}

// sortKeys maps a column to its value and whether larger is better. Missing
// values always sort last regardless of direction.
var sortKeys = map[string]struct {
	value      func(Row) *int64
	largerBest bool
}{
	"score":     {func(r Row) *int64 { return r.Composite }, true},
	"reasoning": {func(r Row) *int64 { return r.Reasoning }, true},
	"coding":    {func(r Row) *int64 { return r.Coding }, true},
	"agent":     {func(r Row) *int64 { return r.Agent }, true},
	"context":   {func(r Row) *int64 { return r.ContextTokens }, true},
	"speed":     {func(r Row) *int64 { return r.SpeedMilli }, true},
	"price":     {func(r Row) *int64 { return r.BlendedPrice }, false},
	"release": {func(r Row) *int64 {
		if r.ReleaseDate == nil {
			return nil
		}
		v := r.ReleaseDate.Unix()
		return &v
	}, true},
}

func (q *Query) Normalize() error {
	if q.Window == "" {
		q.Window = "all"
	}
	if q.Sort == "" {
		q.Sort = "score"
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if _, ok := sortKeys[q.Sort]; !ok {
		return ErrBadQuery
	}
	if q.Window != "all" && q.Window != "90d" && q.Window != "30d" {
		return ErrBadQuery
	}
	if q.Page < 1 || q.Page > MaxPage {
		return ErrBadQuery
	}
	return nil
}

type Page struct {
	Rows     []Row `json:"rows"`
	Total    int   `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// Filter applies a normalised query. Ranks stay the global composite rank so
// a filtered view never renumbers models.
func Filter(s Snapshot, q Query, now time.Time) Page {
	rows := make([]Row, 0, len(s.Rows))
	for _, r := range s.Rows {
		if q.OpenOnly && !r.OpenWeights {
			continue
		}
		if q.Window != "all" {
			days := 90
			if q.Window == "30d" {
				days = 30
			}
			if r.ReleaseDate == nil || r.ReleaseDate.Before(now.AddDate(0, 0, -days)) {
				continue
			}
		}
		rows = append(rows, r)
	}
	key := sortKeys[q.Sort]
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := key.value(rows[i]), key.value(rows[j])
		if (a == nil) != (b == nil) {
			return a != nil
		}
		if a == nil || *a == *b {
			return false // keep the composite order from the snapshot
		}
		better := *a > *b
		if !key.largerBest {
			better = *a < *b
		}
		if q.Asc {
			return !better
		}
		return better
	})
	total := len(rows)
	start := (q.Page - 1) * PageSize
	if start > total {
		start = total
	}
	end := start + PageSize
	if end > total {
		end = total
	}
	return Page{Rows: rows[start:end], Total: total, Page: q.Page, PageSize: PageSize}
}

// Leader is one "best in category" card on the home page.
type Leader struct {
	Category string `json:"category"`
	Row      Row    `json:"row"`
	// Value is the figure the card highlights, in the unit named by Unit.
	Value int64  `json:"value"`
	Unit  string `json:"unit"`
}

// Leaders derives the home cards from the same snapshot as the table, so the
// cards and rows can never disagree. A category with no data is omitted.
func Leaders(s Snapshot) []Leader {
	var out []Leader
	pick := func(category, unit string, rows []Row, value func(Row) *int64, largerBest bool) {
		var best *Row
		var bestValue int64
		for i := range rows {
			v := value(rows[i])
			if v == nil {
				continue
			}
			if best == nil || (largerBest && *v > bestValue) || (!largerBest && *v < bestValue) {
				best, bestValue = &rows[i], *v
			}
		}
		if best != nil {
			out = append(out, Leader{Category: category, Row: *best, Value: bestValue, Unit: unit})
		}
	}
	var topTen, open []Row
	for _, r := range s.Rows {
		if r.Rank != nil && len(topTen) < 10 {
			topTen = append(topTen, r)
		}
		if r.OpenWeights {
			open = append(open, r)
		}
	}
	pick("reasoning", "bps", s.Rows, func(r Row) *int64 { return r.Reasoning }, true)
	pick("coding", "bps", s.Rows, func(r Row) *int64 { return r.Coding }, true)
	pick("value", "micro_usd", topTen, func(r Row) *int64 { return r.BlendedPrice }, false)
	pick("speed", "tokens_per_sec_milli", s.Rows, func(r Row) *int64 { return r.SpeedMilli }, true)
	pick("context", "tokens", s.Rows, func(r Row) *int64 { return r.ContextTokens }, true)
	pick("open", "bps", open, func(r Row) *int64 { return r.Composite }, true)
	return out
}

// Recent lists models released within the last 15 days, newest first.
func Recent(s Snapshot, now time.Time, limit int) []Row {
	var out []Row
	for _, r := range s.Rows {
		if r.ReleaseDate != nil && !r.ReleaseDate.After(now) && now.Sub(*r.ReleaseDate) <= newModelWindow {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReleaseDate.After(*out[j].ReleaseDate) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
