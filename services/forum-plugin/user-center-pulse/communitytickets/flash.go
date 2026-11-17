package communitytickets

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
)

const FlashStartDay = "2026-11-18"
const flashSchema = `CREATE TABLE IF NOT EXISTS metar_checkin_flash (
 user_id BIGINT UNSIGNED NOT NULL, earned_day CHAR(10) NOT NULL,
 event_id BIGINT UNSIGNED NOT NULL UNIQUE, external_id VARCHAR(32) NOT NULL,
 cents INT NOT NULL, grant_id VARCHAR(64) NOT NULL DEFAULT '',
 status VARCHAR(16) NOT NULL DEFAULT 'pending', next_attempt_at BIGINT NOT NULL DEFAULT 0,
 PRIMARY KEY(user_id,earned_day), INDEX pending(status,next_attempt_at),
 CHECK(cents BETWEEN 1 AND 150)
) ENGINE=InnoDB`

type FlashReceipt struct {
	UserID     string
	ExternalID string
	Day        string `json:"day"`
	EventID    uint64 `json:"event_id"`
	Cents      int64  `json:"cents"`
	GrantID    string
	Status     string
}

func RandomFlashCents() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(150))
	if err != nil {
		return 0, err
	}
	return n.Int64() + 1, nil
}

// Generate only from immutable, server-owned EXP check-in facts and protected
// bindings. Random cents commit before the signed delivery request is possible.
func (s *Store) PrepareFlash(ctx context.Context, user string) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	day := Day(s.Now())
	if _, err = tx.ExecContext(ctx, "INSERT INTO metar_ticket_day VALUES(?,?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)", user, day); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.earned_day,b.external_id FROM metar_exp_event e
 JOIN user_external_login b ON b.user_id=e.user_id AND b.provider='pulse_user_center'
 JOIN user u ON u.id=e.user_id
 LEFT JOIN metar_checkin_flash f ON f.user_id=e.user_id AND f.earned_day=e.earned_day
 WHERE e.user_id=? AND e.kind='checkin' AND e.status IN('awarded','capped') AND e.earned_day>=? AND e.occurred_at<=?
 AND e.occurred_at>UNIX_TIMESTAMP(b.created_at) AND u.status=1 AND u.mail_status=1 AND f.user_id IS NULL
 ORDER BY e.id LIMIT 100`, user, FlashStartDay, s.Now().Unix())
	if err != nil {
		return err
	}
	var items []FlashReceipt
	for rows.Next() {
		var v FlashReceipt
		v.UserID = user
		if err = rows.Scan(&v.EventID, &v.Day, &v.ExternalID); err != nil {
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
		cents, err := RandomFlashCents()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO metar_checkin_flash(user_id,earned_day,event_id,external_id,cents) VALUES(?,?,?,?,?)", v.UserID, v.Day, v.EventID, v.ExternalID, cents); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) CollectFlash(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT e.user_id FROM metar_exp_event e
 JOIN user_external_login b ON b.user_id=e.user_id AND b.provider='pulse_user_center'
 JOIN user u ON u.id=e.user_id
 LEFT JOIN metar_checkin_flash f ON f.event_id=e.id
 WHERE e.kind='checkin' AND e.status IN('awarded','capped') AND e.earned_day>=? AND e.occurred_at<=? AND e.occurred_at>UNIX_TIMESTAMP(b.created_at)
 AND u.status=1 AND u.mail_status=1 AND f.event_id IS NULL ORDER BY e.user_id LIMIT 100`, FlashStartDay, s.Now().Unix())
	if err != nil {
		return err
	}
	var users []string
	for rows.Next() {
		var user string
		if err = rows.Scan(&user); err != nil {
			rows.Close()
			return err
		}
		users = append(users, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, user := range users {
		if err = s.PrepareFlash(ctx, user); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) PendingFlash(ctx context.Context) ([]FlashReceipt, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT user_id,external_id,earned_day,event_id,cents,grant_id,status FROM metar_checkin_flash WHERE status IN('pending','blocked') AND next_attempt_at<=? ORDER BY next_attempt_at,event_id LIMIT 50", s.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlashReceipt{}
	for rows.Next() {
		var v FlashReceipt
		if err = rows.Scan(&v.UserID, &v.ExternalID, &v.Day, &v.EventID, &v.Cents, &v.GrantID, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) DeferFlash(ctx context.Context, v FlashReceipt) error {
	delay := int64(30)
	if v.Status == "blocked" {
		delay = 300
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE metar_checkin_flash SET next_attempt_at=? WHERE user_id=? AND earned_day=?", s.Now().Unix()+delay, v.UserID, v.Day)
	return err
}
func (s *Store) SaveFlash(ctx context.Context, v FlashReceipt, id, state string, cents int64) error {
	if cents != v.Cents || id == "" || len(id) > 64 || (v.GrantID != "" && v.GrantID != id) {
		return errors.New("flash receipt conflict")
	}
	if state != "pending" && state != "settled" && state != "blocked" && state != "reversed" {
		return errors.New("invalid flash state")
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE metar_checkin_flash SET grant_id=?,status=? WHERE user_id=? AND earned_day=? AND cents=? AND (grant_id='' OR grant_id=?) AND (status IN('pending','blocked') OR ?='reversed')", id, state, v.UserID, v.Day, cents, id, state)
	return err
}
func (s *Store) Flash(ctx context.Context, user, day string) (FlashReceipt, error) {
	var v FlashReceipt
	err := s.DB.QueryRowContext(ctx, "SELECT user_id,external_id,earned_day,event_id,cents,grant_id,status FROM metar_checkin_flash WHERE user_id=? AND earned_day=?", user, day).Scan(&v.UserID, &v.ExternalID, &v.Day, &v.EventID, &v.Cents, &v.GrantID, &v.Status)
	return v, err
}

func (s *Store) FlashEligible(ctx context.Context, user, day string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_exp_event e JOIN user_external_login b ON b.user_id=e.user_id AND b.provider='pulse_user_center' JOIN user u ON u.id=e.user_id WHERE e.user_id=? AND e.earned_day=? AND e.earned_day>=? AND e.kind='checkin' AND e.status IN('awarded','capped') AND e.occurred_at<=? AND e.occurred_at>UNIX_TIMESTAMP(b.created_at) AND u.status=1 AND u.mail_status=1`, user, day, FlashStartDay, s.Now().Unix()).Scan(&n)
	return n == 1, err
}
