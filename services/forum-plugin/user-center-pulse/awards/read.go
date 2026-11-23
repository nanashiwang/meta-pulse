package awards

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Page struct {
	Period       Period      `json:"period"`
	Status       string      `json:"status"`
	Version      int64       `json:"version"`
	ReviewAt     int64       `json:"review_at"`
	Items        []Candidate `json:"items"`
	More         bool        `json:"more"`
	Limit        int         `json:"limit"`
	Amount       int         `json:"amount"`
	RewardStatus string      `json:"reward_status"`
}

func (s *Store) List(ctx context.Context, user string, p Period, page int, admin bool) (Page, error) {
	if _, err := p.Start(); err != nil || page < 1 || page > 10000 {
		return Page{}, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	if admin {
		if err = active(ctx, tx, user, true, false); err != nil {
			return Page{}, err
		}
	}
	return readAwardPeriod(ctx, tx, p, page, admin)
}

func readAwardPeriod(ctx context.Context, tx *sql.Tx, p Period, page int, admin bool) (Page, error) {
	out := Page{Period: p, Items: []Candidate{}, Status: "open", Limit: p.Limit(), Amount: p.Amount(), RewardStatus: "manual_pending"}
	at, err := p.ReviewAt()
	if err != nil {
		return out, err
	}
	out.ReviewAt = at.Unix()
	err = tx.QueryRowContext(ctx, "SELECT status,version FROM metar_award_period WHERE period_type=? AND period_key=? AND kind=?", p.Type, p.Key, p.Kind).Scan(&out.Status, &out.Version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if !admin && out.Status != "published" {
		return out, nil
	}
	query := "SELECT subject_id,user_id,question_id,answer_id,status,reason,criterion_one,criterion_two FROM metar_award_candidate WHERE period_type=? AND period_key=? AND kind=? ORDER BY created_at,subject_id LIMIT 21 OFFSET ?"
	if !admin || out.Status == "published" {
		query = "SELECT subject_id,user_id,question_id,answer_id,status,reason,1,1 FROM metar_award_winner WHERE period_type=? AND period_key=? AND kind=?"
		if !admin {
			query += " AND status='awarded'"
		}
		query += " ORDER BY user_id LIMIT 21 OFFSET ?"
	}
	rows, err := tx.QueryContext(ctx, query, p.Type, p.Key, p.Kind, (page-1)*20)
	if err != nil {
		return out, err
	}
	items := []Candidate{}
	for rows.Next() {
		var c Candidate
		if err = rows.Scan(&c.SubjectID, &c.UserID, &c.QuestionID, &c.AnswerID, &c.Status, &c.Reason, &c.CriterionOne, &c.CriterionTwo); err != nil {
			rows.Close()
			return out, err
		}
		items = append(items, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.More = len(items) > 20
	if out.More {
		items = items[:20]
	}
	for _, c := range items {
		err = eligible(ctx, tx, p, &c)
		if err != nil && !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNotFound) {
			return out, err
		}
		c.Eligible = err == nil
		if !c.Eligible {
			if !admin {
				continue
			}
			c.Title = "内容不可见、账号不符合条件或采纳已取消"
		}
		if p.Type == "week" && p.Kind == "post" {
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE question_id=?", c.QuestionID).Scan(&c.Nominations); err != nil {
				return out, err
			}
			if admin {
				c.Recommendations, err = listRecommendations(ctx, tx, c.QuestionID, out.Status == "published", false)
				if err != nil {
					return out, err
				}
			}
		}
		if out.Status == "published" {
			c.Amount = p.Amount()
			c.RewardStatus = "manual_pending"
			var paidBy, note string
			var paidAt int64
			paymentErr := tx.QueryRowContext(ctx, "SELECT actor_id,paid_at,note FROM metar_award_manual_payment WHERE period_type=? AND period_key=? AND kind=? AND user_id=?", p.Type, p.Key, p.Kind, c.UserID).Scan(&paidBy, &paidAt, &note)
			if paymentErr == nil {
				c.RewardStatus = "manual_paid"
				if admin {
					c.PaidBy = paidBy
					c.PaidAt = paidAt
					c.PaymentNote = note
				}
			} else if !errors.Is(paymentErr, sql.ErrNoRows) {
				return out, paymentErr
			} else {
				state, at, err := deliveryStatus(ctx, tx, winnerSource(p, c.UserID))
				if err != nil {
					return out, err
				}
				if state != "ready" {
					c.RewardStatus = state
					c.PaidAt = at
				}
			}
		}
		out.Items = append(out.Items, c)
	}
	return out, nil
}

// AwardArchive pages complete published periods, and shares visibility checks
// with a direct period link. Unpublished candidates are never queried.
type AwardArchive struct {
	Periods []Page `json:"periods"`
	More    bool   `json:"more"`
}

const awardArchiveSize = 5

func (s *Store) Archive(ctx context.Context, typ, kind string, page int) (AwardArchive, error) {
	out := AwardArchive{Periods: []Page{}}
	if (typ != "week" && typ != "month") || (kind != "post" && kind != "response") || page < 1 || page > 10000 {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT period_key FROM metar_award_period WHERE period_type=? AND kind=? AND status='published' ORDER BY period_key DESC LIMIT ? OFFSET ?", typ, kind, awardArchiveSize+1, (page-1)*awardArchiveSize)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return out, err
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	rows.Close()
	out.More = len(keys) > awardArchiveSize
	if out.More {
		keys = keys[:awardArchiveSize]
	}
	for _, key := range keys {
		p, err := readAwardPeriod(ctx, tx, Period{Type: typ, Key: key, Kind: kind}, 1, false)
		if err != nil {
			return out, err
		}
		out.Periods = append(out.Periods, p)
	}
	return out, nil
}

type NominationStatus struct {
	CanNominate         bool   `json:"can_nominate"`
	Nominated           bool   `json:"nominated"`
	RecommendationCount int    `json:"recommendation_count"`
	DailyRemaining      int    `json:"daily_remaining"`
	WeeklyRemaining     int    `json:"weekly_remaining"`
	Period              Period `json:"period"`
	ClosesAt            int64  `json:"closes_at"`
}

func (s *Store) Status(ctx context.Context, user, id string) (NominationStatus, error) {
	out := NominationStatus{}
	if !idRE.MatchString(id) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, user, false, false); err != nil {
		return out, err
	}
	var owner string
	var status, show int
	var created time.Time
	err = tx.QueryRowContext(ctx, "SELECT user_id,status,`show`,created_at FROM question WHERE id=?", id).Scan(&owner, &status, &show, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if (status != 1 && status != 2) || show != 1 {
		return out, ErrNotFound
	}
	now := s.Now()
	out.Period = Weekly(created, "post")
	out.ClosesAt = weeklyReviewAt(created).Unix()
	var daily, weekly, nominated int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE user_id=? AND click_day=?", user, now.In(Zone).Format("2006-01-02")).Scan(&daily); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE user_id=? AND click_week=?", user, Week(now).Format("2006-01-02")).Scan(&weekly); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE user_id=? AND question_id=?", user, id).Scan(&nominated); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE question_id=?", id).Scan(&out.RecommendationCount); err != nil {
		return out, err
	}
	out.Nominated = nominated > 0
	out.DailyRemaining = max(0, 2-daily)
	out.WeeklyRemaining = max(0, 10-weekly)
	out.CanNominate = owner != user && !out.Nominated && CanNominate(created, now) && daily < 2 && weekly < 10 && out.RecommendationCount < RecommendationLimit
	return out, nil
}

type Audit struct {
	ActorID   string `json:"actor_id"`
	SubjectID string `json:"subject_id"`
	Operation string `json:"operation"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) Audit(ctx context.Context, user string, p Period) ([]Audit, error) {
	if _, err := p.Start(); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, user, true, false); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT actor_id,subject_id,operation,reason,created_at FROM metar_award_audit WHERE period_type=? AND period_key=? AND kind=? ORDER BY id DESC LIMIT 100", p.Type, p.Key, p.Kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Audit{}
	for rows.Next() {
		var a Audit
		if err = rows.Scan(&a.ActorID, &a.SubjectID, &a.Operation, &a.Reason, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
