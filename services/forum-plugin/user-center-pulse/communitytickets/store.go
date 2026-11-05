// Package communitytickets projects Answer facts into capped, durable ticket
// receipts. Pulse remains the only ticket ledger; this store never writes quota.
package communitytickets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{db, time.Now} }

var zone = time.FixedZone("Asia/Shanghai", 8*3600)

func Day(t time.Time) string { return t.In(zone).Format("2006-01-02") }

var schema = []string{
	`CREATE TABLE IF NOT EXISTS metar_ticket_config(id INT PRIMARY KEY,started_at BIGINT NOT NULL) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_ticket_cursor(stream VARCHAR(16) PRIMARY KEY,last_id BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_ticket_source(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,source_key VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,user_id BIGINT UNSIGNED NOT NULL,earned_day CHAR(10) NOT NULL,kind VARCHAR(16) NOT NULL,stream VARCHAR(16) NOT NULL,source_id BIGINT UNSIGNED NOT NULL,INDEX owner_day(user_id,earned_day)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_ticket_day(user_id BIGINT UNSIGNED NOT NULL,earned_day CHAR(10) NOT NULL,PRIMARY KEY(user_id,earned_day)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_ticket_outbox(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,user_id BIGINT UNSIGNED NOT NULL,external_id VARCHAR(32) NOT NULL,earned_day CHAR(10) NOT NULL,category VARCHAR(16) NOT NULL,ordinal INT NOT NULL,delivered BOOLEAN NOT NULL DEFAULT FALSE,next_attempt_at BIGINT NOT NULL DEFAULT 0,UNIQUE receipt(user_id,earned_day,category,ordinal),INDEX pending(delivered,next_attempt_at,id)) ENGINE=InnoDB`,
}

func (s *Store) Ensure(ctx context.Context) error {
	for _, q := range schema {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_ticket_config VALUES(1,?)`, s.Now().Unix())
	if err != nil {
		return err
	}
	var start int64
	if err = s.DB.QueryRowContext(ctx, `SELECT started_at FROM metar_ticket_config WHERE id=1`).Scan(&start); err != nil {
		return err
	}
	for _, stream := range []string{"answer", "comment", "activity", "checkin"} {
		table, column := stream, "UNIX_TIMESTAMP(created_at)"
		if stream == "checkin" {
			table = "metar_exp_event"
			column = "occurred_at"
		}
		var cursor uint64
		err = s.DB.QueryRowContext(ctx, `SELECT last_id FROM metar_ticket_cursor WHERE stream=?`, stream).Scan(&cursor)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// A standalone consistent read must precede INSERT. INSERT SELECT can
		// take shared locks on Answer tables under MySQL repeatable read.
		err = s.DB.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE `+column+`<? ORDER BY id DESC LIMIT 1`, start).Scan(&cursor)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_ticket_cursor(stream,last_id) VALUES(?,?)`, stream, cursor); err != nil {
			return err
		}
	}
	return nil
}

type Receipt struct {
	ID         uint64 `json:"-"`
	UserID     string `json:"-"`
	ExternalID string `json:"-"`
	Day        string `json:"day"`
	Category   string `json:"category"`
	Ordinal    int    `json:"ordinal"`
}
type Progress struct {
	Day       string `json:"day"`
	Likes     int    `json:"likes"`
	Comments  int    `json:"comments"`
	Received  int    `json:"received"`
	CheckedIn bool   `json:"checked_in"`
	Earned    int    `json:"earned"`
	Delivered int    `json:"delivered"`
}

func (s *Store) Pending(ctx context.Context) ([]Receipt, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,user_id,external_id,earned_day,category,ordinal FROM metar_ticket_outbox WHERE delivered=0 AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT 100`, s.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Receipt{}
	for rows.Next() {
		var r Receipt
		if err = rows.Scan(&r.ID, &r.UserID, &r.ExternalID, &r.Day, &r.Category, &r.Ordinal); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Ack(ctx context.Context, id uint64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE metar_ticket_outbox SET delivered=1 WHERE id=?`, id)
	return err
}
func (s *Store) Progress(ctx context.Context, user string) (Progress, error) {
	p := Progress{Day: Day(s.Now())}
	counts, err := countSources(ctx, s.DB, user, p.Day)
	if err != nil {
		return p, err
	}
	p.Likes = counts["like"]
	p.Comments = counts["comment"]
	p.Received = counts["received"]
	p.CheckedIn = counts["checkin"] > 0
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(delivered),0) FROM metar_ticket_outbox WHERE user_id=? AND earned_day=?`, user, p.Day).Scan(&p.Earned, &p.Delivered)
	return p, err
}

type source struct {
	ID           uint64
	Actor, Owner string
	At           int64
	PostAt       int64
	Object       string
	Valid        bool
}

// Scans by durable source ID, including rejected rows so hidden/deleted tails
// cannot stall progress. Missing tables or changed Answer schema fail closed.
func (s *Store) Collect(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var lock int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('metar_ticket_collect_v1',0)`).Scan(&lock); err != nil {
		return err
	}
	if lock != 1 {
		return nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK('metar_ticket_collect_v1')`)
	}()
	var start int64
	if err = s.DB.QueryRowContext(ctx, `SELECT started_at FROM metar_ticket_config WHERE id=1`).Scan(&start); err != nil {
		return err
	}
	for _, stream := range []string{"answer", "comment", "activity", "checkin"} {
		if err = s.collectStream(ctx, stream, start); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) collectStream(ctx context.Context, stream string, start int64) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_ticket_cursor(stream,last_id) VALUES(?,0)`, stream); err != nil {
		return err
	}
	var cursor uint64
	if err := s.DB.QueryRowContext(ctx, `SELECT last_id FROM metar_ticket_cursor WHERE stream=?`, stream).Scan(&cursor); err != nil {
		return err
	}
	queries := map[string]string{
		"answer":   "SELECT a.id,a.user_id,COALESCE(q.user_id,0),UNIX_TIMESTAMP(a.created_at),COALESCE(UNIX_TIMESTAMP(q.created_at),0),a.id,COALESCE(a.status=1 AND q.status IN(1,2) AND q.`show`=1,0) FROM answer a LEFT JOIN question q ON q.id=a.question_id WHERE a.id>? ORDER BY a.id LIMIT 200",
		"comment":  "SELECT a.id,a.user_id,COALESCE(q.user_id,0),UNIX_TIMESTAMP(a.created_at),COALESCE(UNIX_TIMESTAMP(q.created_at),0),a.id,COALESCE(a.status=1 AND q.status IN(1,2) AND q.`show`=1 AND (a.object_id=q.id OR z.status=1),0) FROM comment a LEFT JOIN question q ON q.id=a.question_id LEFT JOIN answer z ON z.id=a.object_id AND z.question_id=q.id WHERE a.id>? ORDER BY a.id LIMIT 200",
		"activity": "SELECT a.id,a.trigger_user_id,a.user_id,UNIX_TIMESTAMP(a.created_at),COALESCE(UNIX_TIMESTAMP(q.created_at),UNIX_TIMESTAMP(z.created_at),0),CONCAT(c.`key`,':',a.original_object_id),COALESCE(a.cancelled=0 AND ((c.`key`='question.voted_up' AND q.status IN(1,2) AND q.`show`=1) OR (c.`key`='answer.voted_up' AND z.status=1 AND p.status IN(1,2) AND p.`show`=1)),0) FROM activity a LEFT JOIN config c ON c.id=a.activity_type LEFT JOIN question q ON c.`key`='question.voted_up' AND q.id=a.original_object_id LEFT JOIN answer z ON c.`key`='answer.voted_up' AND z.id=a.original_object_id LEFT JOIN question p ON p.id=z.question_id WHERE a.id>? ORDER BY a.id LIMIT 200",
		"checkin":  "SELECT id,user_id,user_id,occurred_at,occurred_at,id,kind='checkin' FROM metar_exp_event WHERE id>? ORDER BY id LIMIT 200",
	}
	rows, err := s.DB.QueryContext(ctx, queries[stream], cursor)
	if err != nil {
		return err
	}
	list := []source{}
	for rows.Next() {
		var e source
		var object sql.NullString
		if err = rows.Scan(&e.ID, &e.Actor, &e.Owner, &e.At, &e.PostAt, &object, &e.Valid); err != nil {
			rows.Close()
			return err
		}
		e.Object = object.String
		list = append(list, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range list {
		if e.Valid && e.At >= start && e.At <= s.Now().Unix() {
			if stream == "checkin" {
				err = s.record(ctx, e, stream, "checkin", e.Actor)
			} else if e.Actor != e.Owner && e.Owner != "0" {
				kind := "comment"
				if stream == "activity" {
					kind = "like"
				}
				err = s.record(ctx, e, stream, kind, e.Actor)
				if err == nil && stream != "activity" {
					err = s.record(ctx, e, stream, "received", e.Owner)
				}
			}
			if err != nil {
				return err
			}
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE metar_ticket_cursor SET last_id=? WHERE stream=?`, e.ID, stream); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) record(ctx context.Context, e source, stream, kind, user string) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var external string
	var bound int64
	err = tx.QueryRowContext(ctx, "SELECT b.external_id,UNIX_TIMESTAMP(b.created_at) FROM user_external_login b JOIN `user` u ON u.id=b.user_id WHERE b.provider='pulse_user_center' AND b.user_id=? AND u.status=1 AND u.mail_status=1", user).Scan(&external, &bound)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Received engagement requires a post authored after binding. For outgoing
	// actions the action itself must be after binding; others' older posts qualify.
	if e.At <= bound || (kind == "received" && e.PostAt <= bound) {
		return nil
	}
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user` WHERE id=? AND status=1 AND mail_status=1", e.Actor).Scan(&active); err != nil {
		return err
	}
	if active != 1 {
		return nil
	}
	day := Day(time.Unix(e.At, 0))
	if _, err = tx.ExecContext(ctx, `INSERT INTO metar_ticket_day VALUES(?,?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)`, user, day); err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%s:%s:%d", kind, user, stream, e.ID)
	if kind == "like" {
		key = "like:" + user + ":" + e.Object
	} // permanent across unlike/re-like and midnight
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO metar_ticket_source(source_key,user_id,earned_day,kind,stream,source_id) VALUES(?,?,?,?,?,?)`, key, user, day, kind, stream, e.ID)
	if err != nil {
		return err
	}
	added, _ := result.RowsAffected()
	if added == 0 {
		return tx.Commit()
	}
	counts, err := countSources(ctx, tx, user, day)
	if err != nil {
		return err
	}
	limits := Entitlements(counts["like"], counts["comment"], counts["received"], counts["checkin"] > 0)
	for _, category := range []string{"received", "participation", "checkin"} {
		for i := 1; i <= limits[category]; i++ {
			if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO metar_ticket_outbox(user_id,external_id,earned_day,category,ordinal) VALUES(?,?,?,?,?)`, user, external, day, category, i); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func Entitlements(likes, comments, received int, checkin bool) map[string]int {
	result := map[string]int{"participation": min(3, likes/40+comments/20), "received": min(2, received/10), "checkin": 0}
	if checkin {
		result["checkin"] = 1
	}
	return result
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func countSources(ctx context.Context, q queryer, user, day string) (map[string]int, error) {
	// Only live sources count towards a new threshold. Already issued receipts are
	// immutable and never release a daily slot when content is later removed.
	rows, err := q.QueryContext(ctx, "SELECT s.kind,COUNT(*) FROM metar_ticket_source s LEFT JOIN answer a ON s.stream='answer' AND a.id=s.source_id LEFT JOIN comment c ON s.stream='comment' AND c.id=s.source_id LEFT JOIN question p ON p.id=COALESCE(a.question_id,c.question_id) LEFT JOIN answer ca ON c.object_id=ca.id AND ca.question_id=p.id LEFT JOIN activity v ON s.stream='activity' AND v.id=s.source_id LEFT JOIN config k ON k.id=v.activity_type LEFT JOIN question vq ON k.`key`='question.voted_up' AND vq.id=v.original_object_id LEFT JOIN answer va ON k.`key`='answer.voted_up' AND va.id=v.original_object_id LEFT JOIN question vp ON vp.id=va.question_id WHERE s.user_id=? AND s.earned_day=? AND (s.stream='checkin' OR (p.status IN(1,2) AND p.`show`=1 AND ((s.stream='answer' AND a.status=1) OR (s.stream='comment' AND c.status=1 AND (c.object_id=p.id OR ca.status=1)))) OR (s.stream='activity' AND v.cancelled=0 AND ((vq.status IN(1,2) AND vq.`show`=1) OR (va.status=1 AND vp.status IN(1,2) AND vp.`show`=1)))) GROUP BY s.kind", user, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err = rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		counts[kind] = n
	}
	return counts, rows.Err()
}

// Persist retry scheduling so disabled accounts cannot starve later receipts.
func (s *Store) Defer(ctx context.Context, id uint64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE metar_ticket_outbox SET next_attempt_at=? WHERE id=? AND delivered=0`, s.Now().Add(time.Minute).Unix(), id)
	return err
}
