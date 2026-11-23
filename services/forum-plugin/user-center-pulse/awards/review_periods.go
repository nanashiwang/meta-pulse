package awards

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func validAwardCategory(typ, kind string, optional bool) bool {
	return (typ == "week" || typ == "month" || optional && typ == "") && (kind == "post" || kind == "response" || optional && kind == "")
}

func recentReviewKeys(typ string, now time.Time) []string {
	keys := make([]string, 0, 6)
	local := now.In(Zone)
	for i := 0; i < 6; i++ {
		if typ == "month" {
			first := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, Zone)
			keys = append(keys, first.AddDate(0, -i, 0).Format("2006-01"))
		} else {
			keys = append(keys, Week(now).AddDate(0, 0, -7*i).Format("2006-01-02"))
		}
	}
	return keys
}

func defaultReviewPeriod(ctx context.Context, tx *sql.Tx, p Period, now time.Time) (Period, error) {
	for _, key := range recentReviewKeys(p.Type, now) {
		p.Key = key
		at, _ := p.ReviewAt()
		if !now.Before(at) {
			break
		}
	}
	// Prefer the newest reviewable period that already contains work or a
	// published list. If no period has been populated, keep the calendar default
	// so the administrator can sync candidates or publish an empty period.
	var key string
	err := tx.QueryRowContext(ctx, "SELECT p.period_key FROM metar_award_period p WHERE p.period_type=? AND p.kind=? AND p.period_key<=? AND (p.status='published' OR EXISTS(SELECT 1 FROM metar_award_candidate c WHERE c.period_type=p.period_type AND c.period_key=p.period_key AND c.kind=p.kind)) ORDER BY p.period_key DESC LIMIT 1", p.Type, p.Kind, p.Key).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return Period{}, err
	}
	p.Key = key
	return p, nil
}
