package awards

import (
	"context"
	"database/sql"
	"time"
)

type Submission struct {
	Kind        string `json:"kind"`
	Subject     string `json:"subject_id"`
	Question    string `json:"question_id"`
	Title       string `json:"title"`
	Excerpt     string `json:"excerpt,omitempty"`
	Week        string `json:"week"`
	CanWithdraw bool   `json:"can_withdraw"`
}

// Monthly submissions follow the award week's Monday, not the date of the click.
func (s *Store) Submissions(ctx context.Context, actor string, p Period) ([]Submission, error) {
	start, err := p.Start()
	if err != nil {
		return nil, err
	}
	end := start.AddDate(0, 0, 7)
	if p.Type == "month" {
		weeks, _ := MonthWeeks(p)
		start, _ = time.ParseInLocation("2006-01-02", weeks[0], Zone)
		end = start.AddDate(0, 0, len(weeks)*7)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, actor, false, false); err != nil {
		return nil, err
	}
	return s.submissions(ctx, tx, actor, start, end)
}
func (s *Store) submissions(ctx context.Context, tx *sql.Tx, actor string, start, end time.Time) ([]Submission, error) {
	out := []Submission{}
	for _, kind := range []string{"post", "response"} {
		query := "SELECT q.id,q.id,q.title,'',q.created_at FROM metar_award_nomination n JOIN question q ON q.id=n.question_id WHERE n.user_id=? AND q.status IN (1,2) AND q.`show`=1 AND q.created_at>=? AND q.created_at<? ORDER BY q.created_at DESC,q.id"
		if kind == "response" {
			query = "SELECT a.id,q.id,q.title,LEFT(COALESCE(a.original_text,''),100),MIN(e.created_at) AS first_accepted FROM question q JOIN answer a ON a.id=q.accepted_answer_id AND a.question_id=q.id JOIN activity e ON e.object_id=a.id AND e.trigger_user_id=q.user_id AND e.user_id=a.user_id JOIN config cfg ON cfg.id=e.activity_type AND cfg.`key`='answer.accepted' WHERE q.user_id=? AND a.user_id<>q.user_id AND q.status IN (1,2) AND q.`show`=1 AND a.status=1 AND a.adopted=2 AND NOT EXISTS (SELECT 1 FROM metar_award_audit w WHERE w.actor_id=q.user_id AND w.subject_id=a.id AND w.kind='response' AND w.operation='withdraw') GROUP BY a.id,q.id,q.title HAVING first_accepted>=? AND first_accepted<? ORDER BY first_accepted DESC,a.id"
		}
		rows, err := tx.QueryContext(ctx, query, actor, start, end)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			v := Submission{Kind: kind}
			var created time.Time
			if err = rows.Scan(&v.Subject, &v.Question, &v.Title, &v.Excerpt, &created); err != nil {
				rows.Close()
				return nil, err
			}
			v.Week = Weekly(created, kind).Key
			v.CanWithdraw = CanNominate(created, s.Now())
			out = append(out, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Store) withdraw(ctx context.Context, tx *sql.Tx, actor string, c Command, now time.Time) error {
	p := c.Period
	at, _ := p.ReviewAt()
	if !now.Before(at) {
		return ErrClosed
	}
	state, _, err := lockPeriod(ctx, tx, p)
	if err != nil {
		return err
	}
	if state != "open" {
		return ErrClosed
	}
	start, _ := p.Start()
	items, err := s.submissions(ctx, tx, actor, start, start.AddDate(0, 0, 7))
	if err != nil {
		return err
	}
	for _, v := range items {
		if v.Kind != p.Kind || v.Subject != c.SubjectID {
			continue
		}
		if p.Kind == "post" {
			if _, err = tx.ExecContext(ctx, "DELETE FROM metar_award_nomination WHERE user_id=? AND question_id=?", actor, v.Subject); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM metar_award_candidate WHERE period_type='week' AND period_key=? AND kind=? AND subject_id=? AND (?='response' OR NOT EXISTS(SELECT 1 FROM metar_award_nomination WHERE question_id=?))", p.Key, p.Kind, v.Subject, p.Kind, v.Subject)
		return err
	}
	return ErrNotFound
}
