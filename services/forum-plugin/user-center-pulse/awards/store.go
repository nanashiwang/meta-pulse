package awards

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{db, time.Now} }
func (s *Store) Ensure(ctx context.Context) error {
	for _, query := range []string{
		"CREATE TABLE IF NOT EXISTS metar_award_title_preference(user_id BIGINT UNSIGNED PRIMARY KEY,title_key VARCHAR(24) NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_manual_payment(period_type VARCHAR(8) NOT NULL,period_key VARCHAR(10) NOT NULL,kind VARCHAR(10) NOT NULL,user_id BIGINT UNSIGNED NOT NULL,actor_id BIGINT UNSIGNED NOT NULL,paid_at BIGINT NOT NULL,note TEXT NOT NULL,PRIMARY KEY(period_type,period_key,kind,user_id)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_period(period_type VARCHAR(8) NOT NULL,period_key VARCHAR(10) NOT NULL,kind VARCHAR(10) NOT NULL,status VARCHAR(16) NOT NULL DEFAULT 'open',version BIGINT NOT NULL DEFAULT 0,published_at BIGINT NOT NULL DEFAULT 0,PRIMARY KEY(period_type,period_key,kind)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_candidate(period_type VARCHAR(8) NOT NULL,period_key VARCHAR(10) NOT NULL,kind VARCHAR(10) NOT NULL,subject_id BIGINT UNSIGNED NOT NULL,user_id BIGINT UNSIGNED NOT NULL,question_id BIGINT UNSIGNED NOT NULL,answer_id BIGINT UNSIGNED NOT NULL DEFAULT 0,status VARCHAR(16) NOT NULL DEFAULT 'pending',reason TEXT NOT NULL,criterion_one TINYINT NOT NULL DEFAULT 0,criterion_two TINYINT NOT NULL DEFAULT 0,review_hash CHAR(64) NOT NULL DEFAULT '',created_at BIGINT NOT NULL,PRIMARY KEY(period_type,period_key,kind,subject_id),KEY by_user(user_id)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_nomination(user_id BIGINT UNSIGNED NOT NULL,question_id BIGINT UNSIGNED NOT NULL,click_day CHAR(10) NOT NULL,click_week CHAR(10) NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(user_id,question_id),KEY daily_quota(user_id,click_day),KEY weekly_quota(user_id,click_week)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_winner(period_type VARCHAR(8) NOT NULL,period_key VARCHAR(10) NOT NULL,kind VARCHAR(10) NOT NULL,user_id BIGINT UNSIGNED NOT NULL,subject_id BIGINT UNSIGNED NOT NULL,question_id BIGINT UNSIGNED NOT NULL,answer_id BIGINT UNSIGNED NOT NULL DEFAULT 0,month_key CHAR(7) NOT NULL,status VARCHAR(20) NOT NULL DEFAULT 'awarded',reason TEXT NOT NULL,amount INT NOT NULL,reward_status VARCHAR(20) NOT NULL DEFAULT 'deferred',created_at BIGINT NOT NULL,PRIMARY KEY(period_type,period_key,kind,user_id),KEY monthly_source(period_type,month_key,kind,user_id,status)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_command(actor_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,fingerprint CHAR(64) NOT NULL,result_json TEXT NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(actor_id,request_key)) ENGINE=InnoDB",
		"CREATE TABLE IF NOT EXISTS metar_award_audit(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,actor_id BIGINT UNSIGNED NOT NULL,period_type VARCHAR(8) NOT NULL,period_key VARCHAR(10) NOT NULL,kind VARCHAR(10) NOT NULL,subject_id BIGINT UNSIGNED NOT NULL DEFAULT 0,operation VARCHAR(16) NOT NULL,reason TEXT NOT NULL,created_at BIGINT NOT NULL,KEY by_period(period_type,period_key,kind,id)) ENGINE=InnoDB",
	} {
		if _, err := s.DB.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
func active(ctx context.Context, tx *sql.Tx, user string, admin, lock bool) error {
	if !idRE.MatchString(user) {
		return ErrForbidden
	}
	suffix := " FOR SHARE"
	if lock {
		suffix = " FOR UPDATE"
	}
	var status, mail int
	err := tx.QueryRowContext(ctx, "SELECT status,mail_status FROM user WHERE id=?"+suffix, user).Scan(&status, &mail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if status != 1 || mail != 1 {
		return ErrForbidden
	}
	if admin {
		rows, err := tx.QueryContext(ctx, "SELECT role_id FROM user_role_rel WHERE user_id=? LIMIT 2 FOR SHARE", user)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return ErrForbidden
		}
		var role int
		if err = rows.Scan(&role); err != nil {
			return err
		}
		if rows.Next() || role != 2 {
			return ErrForbidden
		}
		if err = rows.Err(); err != nil {
			return err
		}
	}
	return nil
}
func lockPeriod(ctx context.Context, tx *sql.Tx, p Period) (string, int64, error) {
	_, err := tx.ExecContext(ctx, "INSERT IGNORE INTO metar_award_period(period_type,period_key,kind) VALUES(?,?,?)", p.Type, p.Key, p.Kind)
	if err != nil {
		return "", 0, err
	}
	var status string
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT status,version FROM metar_award_period WHERE period_type=? AND period_key=? AND kind=? FOR UPDATE", p.Type, p.Key, p.Kind).Scan(&status, &version)
	return status, version, err
}

type Result struct {
	OK      bool  `json:"ok"`
	Version int64 `json:"version"`
	Already bool  `json:"already,omitempty"`
}

func (s *Store) Execute(ctx context.Context, actor, key string, c Command, admin bool) (Result, error) {
	out := Result{OK: true}
	if !keyRE.MatchString(key) || c.Validate() != nil {
		return Result{}, ErrInvalid
	}
	if admin != (c.Op != "nominate" && c.Op != "title") {
		return Result{}, ErrForbidden
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, actor, admin, true); err != nil {
		return Result{}, err
	}
	body, _ := json.Marshal(c)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))
	var old, receipt string
	err = tx.QueryRowContext(ctx, "SELECT fingerprint,result_json FROM metar_award_command WHERE actor_id=? AND request_key=?", actor, key).Scan(&old, &receipt)
	if err == nil {
		if old != fingerprint {
			return Result{}, ErrConflict
		}
		err = json.Unmarshal([]byte(receipt), &out)
		return out, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	now := s.Now()
	p := c.Period
	if c.Op == "nominate" {
		p, out.Already, err = s.nominate(ctx, tx, actor, c.QuestionID, now)
	} else if c.Op == "title" {
		err = s.chooseTitle(ctx, tx, actor, c.TitleKey)
		p = Weekly(now, "post")
	} else {
		var status string
		var version int64
		status, version, err = lockPeriod(ctx, tx, p)
		if err == nil && version != c.Version {
			err = ErrConflict
		}
		if err == nil && c.Op != "revoke" && c.Op != "reconfirm" && c.Op != "paid" && status != "open" {
			err = ErrClosed
		}
		if err == nil {
			switch c.Op {
			case "sync":
				err = s.syncCandidates(ctx, tx, p, now)
			case "select", "reject":
				err = s.review(ctx, tx, actor, c, now)
			case "publish":
				err = s.publish(ctx, tx, p, now)
			case "revoke":
				err = s.revoke(ctx, tx, c, now)
			case "paid":
				err = s.markPaid(ctx, tx, actor, c, now)
			case "reconfirm":
				err = s.reconfirm(ctx, tx, actor, c)
			}
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE metar_award_period SET version=version+1 WHERE period_type=? AND period_key=? AND kind=?", p.Type, p.Key, p.Kind)
			out.Version = version + 1
		}
	}
	if err != nil {
		return Result{}, err
	}
	auditReason := c.Reason
	if c.Op == "title" {
		auditReason = "title=" + c.TitleKey
	}
	subject := c.SubjectID
	if subject == "" {
		subject = c.QuestionID
	}
	if subject == "" {
		subject = "0"
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO metar_award_audit(actor_id,period_type,period_key,kind,subject_id,operation,reason,created_at) VALUES(?,?,?,?,?,?,?,?)", actor, p.Type, p.Key, p.Kind, subject, c.Op, auditReason, now.Unix()); err != nil {
		return Result{}, err
	}
	encoded, _ := json.Marshal(out)
	if _, err = tx.ExecContext(ctx, "INSERT INTO metar_award_command VALUES(?,?,?,?,?)", actor, key, fingerprint, string(encoded), now.Unix()); err != nil {
		return Result{}, err
	}
	err = tx.Commit()
	return out, err
}
func (s *Store) nominate(ctx context.Context, tx *sql.Tx, actor, id string, now time.Time) (Period, bool, error) {
	var owner string
	var status, show int
	var created time.Time
	err := tx.QueryRowContext(ctx, "SELECT user_id,status,`show`,created_at FROM question WHERE id=? FOR SHARE", id).Scan(&owner, &status, &show, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Period{}, false, ErrNotFound
	}
	if err != nil {
		return Period{}, false, err
	}
	p := Weekly(created, "post")
	if owner == actor {
		return p, false, ErrForbidden
	}
	if (status != 1 && status != 2) || show != 1 {
		return p, false, ErrNotFound
	}
	var duplicate int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM metar_award_nomination WHERE user_id=? AND question_id=?", actor, id).Scan(&duplicate)
	if err == nil {
		return p, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return p, false, err
	}
	if !CanNominate(created, now) {
		return p, false, ErrClosed
	}
	day := now.In(Zone).Format("2006-01-02")
	week := Week(now).Format("2006-01-02")
	var daily, weekly int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE user_id=? AND click_day=?", actor, day).Scan(&daily); err != nil {
		return p, false, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_award_nomination WHERE user_id=? AND click_week=?", actor, week).Scan(&weekly); err != nil {
		return p, false, err
	}
	if daily >= 2 || weekly >= 10 {
		return p, false, ErrQuota
	}
	state, _, err := lockPeriod(ctx, tx, p)
	if err != nil {
		return p, false, err
	}
	if state != "open" {
		return p, false, ErrClosed
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO metar_award_nomination VALUES(?,?,?,?,?)", actor, id, day, week, now.Unix()); err != nil {
		return p, false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO metar_award_candidate(period_type,period_key,kind,subject_id,user_id,question_id,reason,created_at) VALUES('week',?,'post',?,?,?,'',?)", p.Key, id, owner, id, now.Unix())
	return p, false, err
}
