package awards

import (
	"context"
	"database/sql"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"time"
)

type Title struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Style     string `json:"style"`
	ExpiresAt int64  `json:"expires_at"`
}
type Badge struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Kind  string `json:"kind"`
}
type HonorRecord struct {
	Period     Period `json:"period"`
	QuestionID string `json:"question_id"`
	AnswerID   string `json:"answer_id"`
	Reason     string `json:"reason"`
	GrantedAt  int64  `json:"granted_at"`
}
type Honors struct {
	Titles          []Title               `json:"titles"`
	Selected        *Title                `json:"selected"`
	Selection       string                `json:"selection"`
	Badges          []Badge               `json:"badges"`
	Records         []HonorRecord         `json:"records"`
	More            bool                  `json:"more"`
	Recommendations []RecommendationHonor `json:"recommendations,omitempty"`
}

func titleKey(kind, period string) string { return kind + "-" + period }
func titleName(kind string) string {
	if kind == "post" {
		return "优秀作者"
	}
	return "学识渊博"
}
func badgeName(kind string) string {
	if kind == "post" {
		return "编辑者"
	}
	return "学者"
}
func validTitle(key string) bool {
	return key == "" || key == "post-week" || key == "post-month" || key == "response-week" || key == "response-month"
}

// Unique award rows are the grant ledger: replay cannot increment badges or extend time.
// Revoked/review-required awards stop contributing; other awards remain intact.
func honors(ctx context.Context, tx *sql.Tx, user string, now time.Time) (Honors, error) {
	out := Honors{Titles: []Title{}, Badges: []Badge{}, Records: []HonorRecord{}}
	rows, err := tx.QueryContext(ctx, "SELECT kind,period_type,MAX(created_at),COUNT(*) FROM metar_award_winner WHERE user_id=? AND status='awarded' GROUP BY kind,period_type ORDER BY kind,period_type", user)
	if err != nil {
		return out, err
	}
	counts := map[string]int{}
	for rows.Next() {
		var kind, period string
		var granted int64
		var count int
		if err = rows.Scan(&kind, &period, &granted, &count); err != nil {
			rows.Close()
			return out, err
		}
		counts[kind] += count
		days := 7
		style := "normal"
		if period == "month" {
			days = 30
			style = "glow"
		}
		expiry := granted + int64(days*86400)
		if expiry > now.Unix() {
			out.Titles = append(out.Titles, Title{titleKey(kind, period), titleName(kind), style, expiry})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, kind := range []string{"post", "response"} {
		if counts[kind] > 0 {
			out.Badges = append(out.Badges, Badge{badgeName(kind), counts[kind], kind})
		}
	}
	err = tx.QueryRowContext(ctx, "SELECT title_key FROM metar_award_title_preference WHERE user_id=?", user).Scan(&out.Selection)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	for i := range out.Titles {
		if out.Titles[i].Key == out.Selection {
			v := out.Titles[i]
			out.Selected = &v
		}
	}
	return out, nil
}
func (s *Store) Honors(ctx context.Context, user, username string, page int) (Honors, error) {
	out := Honors{}
	if page < 1 || page > 10000 {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if username != "" {
		if len(username) > 100 {
			return out, ErrInvalid
		}
		err = tx.QueryRowContext(ctx, "SELECT id FROM user WHERE username=? AND status=1 AND mail_status=1", username).Scan(&user)
		if errors.Is(err, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		if err != nil {
			return out, err
		}
	} else if err = active(ctx, tx, user, false, false); err != nil {
		return out, err
	}
	out, err = honors(ctx, tx, user, s.Now())
	if err != nil {
		return out, err
	}
	if username != "" {
		out.Titles = nil
		out.Selection = ""
		out.Badges = nil
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT period_type,period_key,kind,question_id,answer_id,reason,created_at FROM metar_award_winner WHERE user_id=? AND status='awarded' ORDER BY created_at DESC,period_type,period_key,kind LIMIT 21 OFFSET ?", user, (page-1)*20)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r HonorRecord
		if err = rows.Scan(&r.Period.Type, &r.Period.Key, &r.Period.Kind, &r.QuestionID, &r.AnswerID, &r.Reason, &r.GrantedAt); err != nil {
			return out, err
		}
		out.Records = append(out.Records, r)
	}
	out.More = len(out.Records) > 20
	if out.More {
		out.Records = out.Records[:20]
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	err = addRecommendationHonors(ctx, tx, user, page, &out)
	return out, err
}
func (s *Store) chooseTitle(ctx context.Context, tx *sql.Tx, user, key string) error {
	if !validTitle(key) {
		return ErrInvalid
	}
	if key != "" {
		h, err := honors(ctx, tx, user, s.Now())
		if err != nil {
			return err
		}
		valid := false
		for _, t := range h.Titles {
			if t.Key == key {
				valid = true
			}
		}
		if !valid {
			return ErrForbidden
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO metar_award_title_preference(user_id,title_key) VALUES(?,?) ON DUPLICATE KEY UPDATE title_key=VALUES(title_key)", user, key)
	return err
}
func (s *Store) markPaid(ctx context.Context, tx *sql.Tx, actor string, c Command, now time.Time) error {
	p := c.Period
	var user, status string
	err := tx.QueryRowContext(ctx, "SELECT user_id,status FROM metar_award_winner WHERE period_type=? AND period_key=? AND kind=? AND subject_id=? FOR UPDATE", p.Type, p.Key, p.Kind, c.SubjectID).Scan(&user, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "awarded" {
		return ErrForbidden
	}
	if err := ensureNotQueued(ctx, tx, winnerSource(p, user)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO metar_award_manual_payment(period_type,period_key,kind,user_id,actor_id,paid_at,note) VALUES(?,?,?,?,?,?,?)", p.Type, p.Key, p.Kind, user, actor, now.Unix(), c.Reason)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return ErrConflict
		}
		return err
	}
	return nil
}
