package awards

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Candidate struct {
	PaidAt          int64            `json:"paid_at,omitempty"`
	PaidBy          string           `json:"paid_by,omitempty"`
	PaymentNote     string           `json:"payment_note,omitempty"`
	ReviewHash      string           `json:"-"`
	ContentHash     string           `json:"-"`
	SubjectID       string           `json:"subject_id"`
	UserID          string           `json:"user_id"`
	QuestionID      string           `json:"question_id"`
	AnswerID        string           `json:"answer_id"`
	Status          string           `json:"status"`
	Reason          string           `json:"reason"`
	CriterionOne    bool             `json:"criterion_one"`
	CriterionTwo    bool             `json:"criterion_two"`
	Title           string           `json:"title"`
	Name            string           `json:"name"`
	Eligible        bool             `json:"eligible"`
	Nominations     int              `json:"nominations"`
	Recommendations []Recommendation `json:"recommendations,omitempty"`
	Sources         []Source         `json:"sources,omitempty"`
	Amount          int              `json:"amount,omitempty"`
	RewardStatus    string           `json:"reward_status,omitempty"`
}
type Source struct {
	Week       string `json:"week"`
	QuestionID string `json:"question_id"`
	AnswerID   string `json:"answer_id"`
}

func candidates(ctx context.Context, tx *sql.Tx, p Period) ([]Candidate, error) {
	rows, err := tx.QueryContext(ctx, "SELECT subject_id,user_id,question_id,answer_id,status,reason,criterion_one,criterion_two,review_hash FROM metar_award_candidate WHERE period_type=? AND period_key=? AND kind=? ORDER BY created_at,subject_id", p.Type, p.Key, p.Kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		if err = rows.Scan(&c.SubjectID, &c.UserID, &c.QuestionID, &c.AnswerID, &c.Status, &c.Reason, &c.CriterionOne, &c.CriterionTwo, &c.ReviewHash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func sourceWeeks(ctx context.Context, tx *sql.Tx, p Period, user string) ([]Source, error) {
	rows, err := tx.QueryContext(ctx, "SELECT period_key,question_id,answer_id FROM metar_award_winner WHERE period_type='week' AND month_key=? AND kind=? AND user_id=? AND status='awarded' ORDER BY period_key FOR SHARE", p.Key, p.Kind, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var s Source
		if err = rows.Scan(&s.Week, &s.QuestionID, &s.AnswerID); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func contentEligible(ctx context.Context, tx *sql.Tx, c *Candidate, kind string) error {
	var status, show int
	var owner, body string
	err := tx.QueryRowContext(ctx, "SELECT title,user_id,status,`show`,COALESCE(original_text,'') FROM question WHERE id=?", c.QuestionID).Scan(&c.Title, &owner, &status, &show, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if (status != 1 && status != 2) || show != 1 {
		return ErrNotFound
	}
	c.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(c.Title+"\x00"+body)))
	if kind == "post" {
		if owner != c.UserID {
			return ErrForbidden
		}
	} else {
		var answerOwner, qid, acceptedID, answerBody string
		var answerStatus, accepted int
		err = tx.QueryRowContext(ctx, "SELECT user_id,question_id,status,adopted,COALESCE(original_text,'') FROM answer WHERE id=?", c.AnswerID).Scan(&answerOwner, &qid, &answerStatus, &accepted, &answerBody)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		c.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(c.ContentHash+"\x00"+answerBody)))
		if answerOwner != c.UserID || qid != c.QuestionID || owner == answerOwner || answerStatus != 1 || accepted != 2 {
			return ErrNotFound
		}
		if err = tx.QueryRowContext(ctx, "SELECT accepted_answer_id FROM question WHERE id=?", c.QuestionID).Scan(&acceptedID); err != nil {
			return err
		}
		if acceptedID != c.AnswerID {
			return ErrNotFound
		}
	}
	if err = active(ctx, tx, c.UserID, false, false); err != nil {
		return err
	}
	return tx.QueryRowContext(ctx, "SELECT COALESCE(NULLIF(display_name,''),username) FROM user WHERE id=?", c.UserID).Scan(&c.Name)
}
func eligible(ctx context.Context, tx *sql.Tx, p Period, c *Candidate) error {
	if p.Type == "week" {
		return contentEligible(ctx, tx, c, p.Kind)
	}
	sources, err := sourceWeeks(ctx, tx, p, c.UserID)
	if err != nil {
		return err
	}
	c.Sources = []Source{}
	c.ContentHash = ""
	for _, source := range sources {
		copy := *c
		copy.QuestionID = source.QuestionID
		copy.AnswerID = source.AnswerID
		if err = contentEligible(ctx, tx, &copy, p.Kind); err == nil {
			c.Sources = append(c.Sources, source)
			c.ContentHash += source.Week + ":" + copy.ContentHash + ";"
			if len(c.Sources) == 1 {
				c.QuestionID = copy.QuestionID
				c.AnswerID = copy.AnswerID
				c.Title = copy.Title
				c.Name = copy.Name
			}
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrForbidden) {
			return err
		}
	}
	c.ContentHash = fmt.Sprintf("%x", sha256.Sum256([]byte(c.ContentHash)))
	if len(c.Sources) == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) syncCandidates(ctx context.Context, tx *sql.Tx, p Period, now time.Time) error {
	start, _ := p.Start()
	if start.After(now) {
		return ErrNotReady
	}
	if p.Type == "month" {
		at, _ := p.ReviewAt()
		if now.Before(at) {
			return ErrNotReady
		}
		weeks, _ := MonthWeeks(p)
		for _, week := range weeks {
			var state string
			err := tx.QueryRowContext(ctx, "SELECT status FROM metar_award_period WHERE period_type='week' AND period_key=? AND kind=?", week, p.Kind).Scan(&state)
			if errors.Is(err, sql.ErrNoRows) || state != "published" {
				return ErrNotReady
			}
			if err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT IGNORE INTO metar_award_candidate(period_type,period_key,kind,subject_id,user_id,question_id,answer_id,reason,created_at) SELECT 'month',?,kind,user_id,user_id,MIN(question_id),0,'',? FROM metar_award_winner WHERE period_type='week' AND month_key=? AND kind=? AND status='awarded' GROUP BY kind,user_id", p.Key, now.Unix(), p.Key, p.Kind)
		return err
	}
	if p.Kind != "response" {
		return nil
	}
	// Answer retains activity.created_at when an answer is unaccepted/reaccepted.
	// Use the first author-triggered acceptance, never an answer's editable updated_at.
	rows, err := tx.QueryContext(ctx, "SELECT a.id,a.user_id,a.question_id,MIN(e.created_at) FROM activity e JOIN config cfg ON cfg.id=e.activity_type AND cfg.`key`='answer.accepted' JOIN answer a ON a.id=e.object_id JOIN question q ON q.id=a.question_id WHERE e.trigger_user_id=q.user_id AND e.user_id=a.user_id AND a.user_id<>q.user_id GROUP BY a.id,a.user_id,a.question_id HAVING MIN(e.created_at)>=? AND MIN(e.created_at)<?", start, start.AddDate(0, 0, 7))
	if err != nil {
		return err
	}
	type accepted struct {
		a, u, q string
		t       time.Time
	}
	items := []accepted{}
	for rows.Next() {
		var item accepted
		if err = rows.Scan(&item.a, &item.u, &item.q, &item.t); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO metar_award_candidate(period_type,period_key,kind,subject_id,user_id,question_id,answer_id,reason,created_at) SELECT 'week',?,'response',?,?,?,?,'',? FROM DUAL WHERE NOT EXISTS (SELECT 1 FROM metar_award_audit WHERE period_type='week' AND period_key=? AND kind='response' AND operation='withdraw' AND subject_id=?)", p.Key, item.a, item.u, item.q, item.a, item.t.Unix(), p.Key, item.a); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) review(ctx context.Context, tx *sql.Tx, actor string, c Command, now time.Time) error {
	at, _ := c.Period.ReviewAt()
	if now.Before(at) {
		return ErrNotReady
	}
	items, err := candidates(ctx, tx, c.Period)
	if err != nil {
		return err
	}
	var chosen *Candidate
	count := 0
	for i := range items {
		if items[i].SubjectID == c.SubjectID {
			chosen = &items[i]
		}
		if items[i].Status == "selected" {
			count++
		}
	}
	if chosen == nil {
		return ErrNotFound
	}
	if chosen.UserID == actor {
		return ErrForbidden
	}
	state := "rejected"
	if c.Op == "select" {
		state = "selected"
		if err = eligible(ctx, tx, c.Period, chosen); err != nil {
			return err
		}
		if chosen.Status != "selected" && count >= c.Period.Limit() {
			return ErrQuota
		}
		for _, item := range items {
			if item.SubjectID != chosen.SubjectID && item.UserID == chosen.UserID && item.Status == "selected" {
				return ErrConflict
			}
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE metar_award_candidate SET status=?,reason=?,criterion_one=?,criterion_two=?,review_hash=? WHERE period_type=? AND period_key=? AND kind=? AND subject_id=?", state, c.Reason, c.CriterionOne, c.CriterionTwo, chosen.ContentHash, c.Period.Type, c.Period.Key, c.Period.Kind, c.SubjectID)
	return err
}
func (s *Store) publish(ctx context.Context, tx *sql.Tx, p Period, now time.Time) error {
	at, _ := p.ReviewAt()
	if now.Before(at) {
		return ErrNotReady
	}
	if err := s.syncCandidates(ctx, tx, p, now); err != nil {
		return err
	}
	items, err := candidates(ctx, tx, p)
	if err != nil {
		return err
	}
	count := 0
	seen := map[string]bool{}
	for _, c := range items {
		if c.Status != "selected" {
			continue
		}
		count++
		if count > p.Limit() || seen[c.UserID] {
			return ErrQuota
		}
		seen[c.UserID] = true
		if !c.CriterionOne || !c.CriterionTwo || c.Reason == "" {
			return ErrInvalid
		}
		if err = eligible(ctx, tx, p, &c); err != nil {
			return err
		}
		if c.ReviewHash != c.ContentHash {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO metar_award_winner(period_type,period_key,kind,user_id,subject_id,question_id,answer_id,month_key,reason,amount,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)", p.Type, p.Key, p.Kind, c.UserID, c.SubjectID, c.QuestionID, c.AnswerID, p.Key[:7], c.Reason, p.Amount(), now.Unix())
		if err != nil {
			return err
		}
		if err = freezeRecommendationRewards(ctx, tx, p, c); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE metar_award_period SET status='published',published_at=? WHERE period_type=? AND period_key=? AND kind=?", now.Unix(), p.Type, p.Key, p.Kind)
	return err
}
func (s *Store) revoke(ctx context.Context, tx *sql.Tx, c Command, now time.Time) error {
	p := c.Period
	var user string
	err := tx.QueryRowContext(ctx, "SELECT user_id FROM metar_award_winner WHERE period_type=? AND period_key=? AND kind=? AND subject_id=? AND status IN ('awarded','review_required') FOR UPDATE", p.Type, p.Key, p.Kind, c.SubjectID).Scan(&user)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metar_award_winner SET status='revoked',reason=? WHERE period_type=? AND period_key=? AND kind=? AND user_id=?", c.Reason, p.Type, p.Key, p.Kind, user); err != nil {
		return err
	}
	if p.Type == "week" {
		// A revoked source must not leave a month award presented as confirmed.
		// Preserve history and require an explicit human review; no financial mutation.
		_, err = tx.ExecContext(ctx, "UPDATE metar_award_winner SET status='review_required' WHERE period_type='month' AND month_key=? AND kind=? AND user_id=? AND status='awarded'", p.Key[:7], p.Kind, user)
	}
	return err
}

func (s *Store) reconfirm(ctx context.Context, tx *sql.Tx, actor string, cmd Command) error {
	p := cmd.Period
	if p.Type != "month" {
		return ErrInvalid
	}
	var c Candidate
	err := tx.QueryRowContext(ctx, "SELECT subject_id,user_id,question_id,answer_id FROM metar_award_winner WHERE period_type='month' AND period_key=? AND kind=? AND subject_id=? AND status='review_required' FOR UPDATE", p.Key, p.Kind, cmd.SubjectID).Scan(&c.SubjectID, &c.UserID, &c.QuestionID, &c.AnswerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if c.UserID == actor {
		return ErrForbidden
	}
	if err = eligible(ctx, tx, p, &c); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE metar_award_winner SET status='awarded',reason=?,question_id=?,answer_id=? WHERE period_type='month' AND period_key=? AND kind=? AND user_id=?", cmd.Reason, c.QuestionID, c.AnswerID, p.Key, p.Kind, c.UserID)
	return err
}
