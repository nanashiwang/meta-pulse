package growth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Collect scans bounded batches of Answer facts and revisits pending/awarded
// events. A DB lease and durable cursors make multiple forum replicas safe.
// It never hooks into or blocks Answer's content-write transaction.
func (s *Store) Collect(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var lock int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('metar_exp_collect_v1',0)`).Scan(&lock); err != nil {
		return err
	}
	if lock != 1 {
		return nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK('metar_exp_collect_v1')`)
	}()
	cfg, err := s.Settings(ctx)
	if err != nil {
		return err
	}
	for _, kind := range []string{"question", "answer", "activity"} {
		if err = s.collectStream(ctx, kind, cfg); err != nil {
			return err
		}
	}
	return s.reconcile(ctx)
}
func (s *Store) cursor(ctx context.Context, name string) (uint64, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_exp_cursor(name,last_id) VALUES(?,0)`, name)
	if err != nil {
		return 0, err
	}
	var id uint64
	err = s.DB.QueryRowContext(ctx, `SELECT last_id FROM metar_exp_cursor WHERE name=?`, name).Scan(&id)
	return id, err
}
func (s *Store) advance(ctx context.Context, name string, id uint64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE metar_exp_cursor SET last_id=? WHERE name=?`, id, name)
	return err
}
func (s *Store) collectStream(ctx context.Context, stream string, cfg Settings) error {
	cursor, err := s.cursor(ctx, stream)
	if err != nil {
		return err
	}
	query := `SELECT id,user_id,id,0,UNIX_TIMESTAMP(created_at),'` + stream + `','` + stream + `' FROM ` + stream + ` WHERE id>? ORDER BY id LIMIT 200`
	if stream == "activity" {
		query = "SELECT a.id,a.user_id,a.original_object_id,a.trigger_user_id,UNIX_TIMESTAMP(a.created_at),CASE WHEN c.`key`='answer.accepted' THEN 'accepted' ELSE 'like' END,CASE WHEN c.`key`='question.voted_up' THEN 'question' ELSE 'answer' END FROM activity a JOIN config c ON c.id=a.activity_type WHERE a.id>? AND c.`key` IN ('answer.accepted','answer.voted_up','question.voted_up') ORDER BY a.id LIMIT 200"
	}
	rows, err := s.DB.QueryContext(ctx, query, cursor)
	if err != nil {
		return err
	}
	type item struct {
		id uint64
		e  Event
	}
	items := []item{}
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.e.UserID, &v.e.ObjectID, &v.e.ActorID, &v.e.OccurredAt, &v.e.Kind, &v.e.ObjectType); err != nil {
			rows.Close()
			return err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		e := v.e
		if e.OccurredAt >= cfg.StartedAt && e.OccurredAt <= s.Now().Unix() {
			rule, _ := cfg.Rules.For(e.Kind)
			e.Source = e.Kind + ":" + e.ObjectType + ":" + e.ObjectID
			if e.Kind == "like" {
				e.Source += ":" + e.ActorID
			}
			e.EligibleAt = e.OccurredAt + 86400
			e.Day = Day(time.Unix(e.EligibleAt, 0))
			e.Amount = rule.Amount
			e.DailyCap = cfg.Rules.DailyCap
			e.DailyAmount = rule.DailyAmount
			e.DailyCount = rule.DailyCount
			e.RuleVersion = cfg.Version
			e.Reason = map[string]string{"question": "有效问题", "answer": "有效回答", "like": "内容获得认可", "accepted": "回答被采纳"}[e.Kind]
			err = s.transaction(ctx, e.UserID, func(tx *sql.Tx, a *Account) error {
				event, err := register(ctx, tx, e)
				if err != nil {
					return err
				}
				valid, err := sourceValid(ctx, tx, event)
				if err != nil {
					return err
				}
				if !valid {
					return revoke(ctx, tx, a, event, "内容或互动不符合经验规则", s.Now())
				}
				return settle(ctx, tx, a, event, s.Now())
			})
			// An inactive author never earns from this event, even after reactivation.
			if err != nil && !errors.Is(err, ErrForbidden) {
				return err
			}
		}
		if err = s.advance(ctx, stream, v.id); err != nil {
			return err
		}
	}
	return nil
}
func sourceValid(ctx context.Context, tx *sql.Tx, e Event) (bool, error) {
	if e.ObjectType != "question" && e.ObjectType != "answer" {
		return false, ErrInvalid
	}
	var owner, parentOwner string
	var status, show int
	var accepted string
	query := "SELECT user_id,status,`show`,accepted_answer_id,user_id FROM question WHERE id=?"
	if e.ObjectType == "answer" {
		query = "SELECT a.user_id,IF(q.status IN (1,2),a.status,10),q.`show`,q.accepted_answer_id,q.user_id FROM answer a JOIN question q ON q.id=a.question_id WHERE a.id=?"
	}
	err := tx.QueryRowContext(ctx, query, e.ObjectID).Scan(&owner, &status, &show, &accepted, &parentOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if owner != e.UserID || show != 1 || (status != 1 && !(e.ObjectType == "question" && status == 2)) {
		return false, nil
	}
	if e.Kind == "accepted" {
		return accepted == e.ObjectID && parentOwner != e.UserID && e.ActorID == parentOwner, nil
	}
	if e.Kind != "like" {
		return true, nil
	}
	if e.ActorID == e.UserID || !validID(e.ActorID) {
		return false, nil
	}
	var count int
	key := e.ObjectType + ".voted_up"
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type WHERE c.`key`=? AND a.original_object_id=? AND a.user_id=? AND a.trigger_user_id=? AND a.cancelled=0", key, e.ObjectID, e.UserID, e.ActorID).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	var mail, userStatus int
	var created int64
	if err = tx.QueryRowContext(ctx, "SELECT mail_status,status,UNIX_TIMESTAMP(created_at) FROM `user` WHERE id=?", e.ActorID).Scan(&mail, &userStatus, &created); errors.Is(err, sql.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	// Reputation cannot substitute for experience; inspect the actor's ledger at
	// the time of the vote so delayed scans cannot qualify an earlier farm vote.
	var experience int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM metar_exp_ledger WHERE user_id=? AND created_at<=?`, e.ActorID, e.OccurredAt).Scan(&experience); err != nil {
		return false, err
	}
	return mail == 1 && userStatus == 1 && e.OccurredAt-created >= 7*86400 && experience >= Levels[1].Minimum, nil
}
func (s *Store) reconcile(ctx context.Context) error {
	cursor, err := s.cursor(ctx, "reconcile")
	if err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+eventColumns+` FROM metar_exp_event WHERE id>? AND kind IN ('question','answer','like','accepted','featured') AND status IN ('pending','awarded') ORDER BY id LIMIT 200`, cursor)
	if err != nil {
		return err
	}
	events := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return s.advance(ctx, "reconcile", 0)
	}
	for _, e := range events {
		err = s.transaction(ctx, e.UserID, func(tx *sql.Tx, a *Account) error {
			fresh, err := scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM metar_exp_event WHERE id=? FOR UPDATE`, e.ID))
			if err != nil {
				return err
			}
			valid, err := sourceValid(ctx, tx, fresh)
			if err != nil {
				return err
			}
			if !valid {
				return revoke(ctx, tx, a, fresh, "内容删除、隐藏或互动已撤销", s.Now())
			}
			return settle(ctx, tx, a, fresh, s.Now())
		})
		if err != nil && !errors.Is(err, ErrForbidden) {
			return err
		}
		if err = s.advance(ctx, "reconcile", e.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Feature(ctx context.Context, actor, key, kind, id, reason string) error {
	if (kind != "question" && kind != "answer") || !validID(id) {
		return ErrInvalid
	}
	if err := s.CheckAdmin(ctx, actor); err != nil {
		return err
	}
	var user string
	query := "SELECT user_id FROM " + kind + " WHERE id=?"
	if err := s.DB.QueryRowContext(ctx, query, id).Scan(&user); err != nil {
		return err
	}
	err := s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		replay, err := audit(ctx, tx, actor, key, "featured", reason, []any{kind, id, reason}, s.Now())
		if err != nil || replay {
			return err
		}
		cfg, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		now := s.Now()
		e, err := register(ctx, tx, Event{Source: fmt.Sprintf("featured:%s:%s", kind, id), UserID: user, Kind: "featured", ObjectType: kind, ObjectID: id, ActorID: "0", OccurredAt: now.Unix(), EligibleAt: now.Unix(), Day: Day(now), Amount: cfg.Rules.Featured, DailyCount: cfg.Rules.FeaturedMonthlyCount, DailyAmount: cfg.Rules.Featured * int64(cfg.Rules.FeaturedMonthlyCount), RuleVersion: cfg.Version, Reason: reason})
		if err != nil {
			return err
		}
		valid, err := sourceValid(ctx, tx, e)
		if err != nil {
			return err
		}
		if !valid {
			return ErrInvalid
		}
		return settle(ctx, tx, a, e, now)
	})
	if err != nil {
		return err
	}
	var status string
	if err = s.DB.QueryRowContext(ctx, `SELECT status FROM metar_exp_event WHERE source_key=?`, "featured:"+kind+":"+id).Scan(&status); err != nil {
		return err
	}
	if status != "awarded" {
		return ErrConflict
	}
	return nil
}
