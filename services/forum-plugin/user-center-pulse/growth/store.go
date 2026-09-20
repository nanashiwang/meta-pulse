package growth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Account struct {
	Balance     int64  `json:"experience"`
	LastCheckin string `json:"last_checkin"`
	Streak      int    `json:"streak"`
	Appearance  string `json:"appearance"`
}
type Event struct {
	ID          uint64 `json:"id"`
	Source      string `json:"-"`
	UserID      string `json:"-"`
	Kind        string `json:"kind"`
	ObjectType  string `json:"object_type"`
	ObjectID    string `json:"object_id"`
	ActorID     string `json:"-"`
	OccurredAt  int64  `json:"occurred_at"`
	EligibleAt  int64  `json:"eligible_at"`
	Day         string `json:"earned_day"`
	Amount      int64  `json:"amount"`
	DailyCap    int64  `json:"-"`
	DailyCount  int    `json:"-"`
	DailyAmount int64  `json:"-"`
	RuleVersion int64  `json:"rule_version"`
	Status      string `json:"status"`
	Fingerprint string `json:"-"`
	Reason      string `json:"reason"`
}
type Entry struct {
	ID         uint64 `json:"id"`
	Delta      int64  `json:"delta"`
	Kind       string `json:"kind"`
	Balance    int64  `json:"balance"`
	Reason     string `json:"reason"`
	CreatedAt  int64  `json:"created_at"`
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}
type Notice struct {
	ID        uint64 `json:"id"`
	Level     int    `json:"level"`
	CreatedAt int64  `json:"created_at"`
}
type Summary struct {
	Account
	Level         Level    `json:"level"`
	Levels        []Level  `json:"levels"`
	Next          *Level   `json:"next_level"`
	Today         int64    `json:"today"`
	DailyCap      int64    `json:"daily_cap"`
	CheckinAmount int64    `json:"checkin_amount"`
	Notices       []Notice `json:"notices"`
	Rules         Rules    `json:"rules"`
	RuleVersion   int64    `json:"rule_version"`
}

func fingerprint(v any) string {
	data, _ := json.Marshal(v)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
func validID(id string) bool {
	if id == "" || id[0] == '0' {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(id) <= 19
}
func lockAccount(ctx context.Context, tx *sql.Tx, user string) (Account, error) {
	var a Account
	if !validID(user) {
		return a, ErrInvalid
	}
	// Locking the account serializes daily quotas, check-in and all event replays
	// across every forum instance. Live Answer status is read in this transaction.
	if _, err := tx.ExecContext(ctx, `INSERT INTO metar_exp_account(user_id) VALUES(?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)`, user); err != nil {
		return a, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT balance,last_checkin,streak,appearance FROM metar_exp_account WHERE user_id=? FOR UPDATE`, user).Scan(&a.Balance, &a.LastCheckin, &a.Streak, &a.Appearance); err != nil {
		return a, err
	}
	var status, mail int
	if err := tx.QueryRowContext(ctx, "SELECT status,mail_status FROM `user` WHERE id=?", user).Scan(&status, &mail); err != nil {
		return a, err
	}
	if status != 1 || mail != 1 {
		return a, ErrForbidden
	}
	return a, nil
}
func (s *Store) transaction(ctx context.Context, user string, fn func(*sql.Tx, *Account) error) error {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := lockAccount(ctx, tx, user)
	if err != nil {
		return err
	}
	if err = fn(tx, &a); err != nil {
		return err
	}
	return tx.Commit()
}

const eventColumns = `id,source_key,user_id,kind,object_type,object_id,actor_id,occurred_at,eligible_at,earned_day,amount,daily_cap,daily_count,daily_amount,rule_version,status,fingerprint,reason`

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.Source, &e.UserID, &e.Kind, &e.ObjectType, &e.ObjectID, &e.ActorID, &e.OccurredAt, &e.EligibleAt, &e.Day, &e.Amount, &e.DailyCap, &e.DailyCount, &e.DailyAmount, &e.RuleVersion, &e.Status, &e.Fingerprint, &e.Reason)
	return e, err
}
func register(ctx context.Context, tx *sql.Tx, e Event) (Event, error) {
	if len(e.Source) == 0 || len(e.Source) > 191 || e.Amount < 1 || e.Amount > 1000000 || !validID(e.UserID) {
		return Event{}, ErrInvalid
	}
	if e.ObjectID == "" {
		e.ObjectID = "0"
	}
	if e.ActorID == "" {
		e.ActorID = "0"
	}
	e.Status = "pending"
	// Stable business identity. Rule changes cannot change an existing event's
	// promised amount or make old content earn a second time.
	e.Fingerprint = fingerprint([]any{e.Source, e.UserID, e.Kind, e.ObjectType, e.ObjectID, e.ActorID})
	_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO metar_exp_event(source_key,user_id,kind,object_type,object_id,actor_id,occurred_at,eligible_at,earned_day,amount,daily_cap,daily_count,daily_amount,rule_version,status,fingerprint,reason) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.Source, e.UserID, e.Kind, e.ObjectType, e.ObjectID, e.ActorID, e.OccurredAt, e.EligibleAt, e.Day, e.Amount, e.DailyCap, e.DailyCount, e.DailyAmount, e.RuleVersion, e.Status, e.Fingerprint, e.Reason)
	if err != nil {
		return Event{}, err
	}
	out, err := scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM metar_exp_event WHERE source_key=? FOR UPDATE`, e.Source))
	if err == nil && (out.Fingerprint != e.Fingerprint || (e.Kind == "pulse" && out.Amount != e.Amount)) {
		return out, ErrConflict
	}
	return out, err
}
func appendLedger(ctx context.Context, tx *sql.Tx, a *Account, e Event, delta int64, kind, reason, actor string, now time.Time) error {
	if delta > 0 && a.Balance > math.MaxInt64-delta || delta < 0 && a.Balance < math.MinInt64-delta {
		return ErrInvalid
	}
	next := a.Balance + delta
	_, err := tx.ExecContext(ctx, `INSERT INTO metar_exp_ledger(entry_key,user_id,event_id,delta,kind,balance,reason,actor_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, kind+":"+e.Source, e.UserID, e.ID, delta, kind, next, reason, actor, now.Unix())
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE metar_exp_account SET balance=? WHERE user_id=?`, next, e.UserID); err != nil {
		return err
	}
	previous := LevelFor(a.Balance).Number
	a.Balance = next
	for level := previous + 1; level <= LevelFor(next).Number; level++ {
		if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO metar_exp_notice(user_id,level,created_at) VALUES(?,?,?)`, e.UserID, level, now.Unix()); err != nil {
			return err
		}
	}
	return nil
}
func settle(ctx context.Context, tx *sql.Tx, a *Account, e Event, now time.Time) error {
	if e.Status != "pending" || e.EligibleAt > now.Unix() {
		return nil
	}
	var used, count, total int64
	if e.Kind != "pulse" && e.Kind != "adjustment" {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.delta),0),COUNT(*) FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE e.user_id=? AND e.earned_day=? AND e.kind=? AND l.delta>0 AND l.kind='award'`, e.UserID, e.Day, e.Kind).Scan(&used, &count); err != nil {
			return err
		}
		if e.Kind == "featured" {
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE e.user_id=? AND e.earned_day LIKE ? AND e.kind='featured' AND l.kind='award'`, e.UserID, e.Day[:7]+"%").Scan(&count); err != nil {
				return err
			}
			used = 0
		}
		if e.Kind != "featured" {
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.delta),0) FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE e.user_id=? AND e.earned_day=? AND e.kind NOT IN ('pulse','featured','adjustment') AND l.delta>0 AND l.kind='award'`, e.UserID, e.Day).Scan(&total); err != nil {
				return err
			}
		}
		capped := count >= int64(e.DailyCount) || used+e.Amount > e.DailyAmount || (e.Kind != "featured" && total+e.Amount > e.DailyCap)
		if e.Kind == "like" {
			var pair int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE e.user_id=? AND e.actor_id=? AND e.earned_day=? AND e.kind='like' AND l.kind='award'`, e.UserID, e.ActorID, e.Day).Scan(&pair); err != nil {
				return err
			}
			capped = capped || pair >= 3
		}
		if capped {
			_, err := tx.ExecContext(ctx, `UPDATE metar_exp_event SET status='capped' WHERE id=?`, e.ID)
			return err
		}
	}
	if err := appendLedger(ctx, tx, a, e, e.Amount, "award", e.Reason, e.ActorID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE metar_exp_event SET status='awarded' WHERE id=?`, e.ID)
	return err
}
func (s *Store) Checkin(ctx context.Context, user string) error {
	now := s.Now()
	return s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		cfg, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		amount, streak := CheckinReward(cfg.Rules, a.LastCheckin, a.Streak, now)
		if amount == 0 {
			return nil
		}
		e, err := register(ctx, tx, Event{Source: "checkin:" + user + ":" + Day(now), UserID: user, Kind: "checkin", OccurredAt: now.Unix(), EligibleAt: now.Unix(), Day: Day(now), Amount: amount, DailyCap: cfg.Rules.DailyCap, DailyCount: 1, DailyAmount: amount, RuleVersion: cfg.Version, Reason: "每日签到"})
		if err != nil {
			return err
		}
		if err = settle(ctx, tx, a, e, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE metar_exp_account SET last_checkin=?,streak=? WHERE user_id=?`, Day(now), streak, user)
		return err
	})
}
func (s *Store) Summary(ctx context.Context, user string) (Summary, error) {
	out := Summary{Notices: []Notice{}}
	err := s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		cfg, err := settings(ctx, tx)
		if err != nil {
			return err
		}
		out.Account = *a
		if out.Balance < 0 {
			out.Balance = 0
		}
		out.Level = LevelFor(out.Balance)
		out.Levels = Levels
		if out.Level.Number < 8 {
			v := Levels[out.Level.Number+1]
			out.Next = &v
		}
		out.DailyCap = cfg.Rules.DailyCap
		out.Rules = cfg.Rules
		out.RuleVersion = cfg.Version
		if a.LastCheckin != Day(s.Now()) && a.LastCheckin != Day(s.Now().AddDate(0, 0, -1)) {
			out.Streak = 0
		}
		out.CheckinAmount, _ = CheckinReward(cfg.Rules, a.LastCheckin, a.Streak, s.Now())
		if !AppearanceAllowed(out.Appearance, out.Level.Number) {
			out.Appearance = "default"
		}
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(l.delta),0) FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE e.user_id=? AND e.earned_day=? AND e.kind NOT IN ('pulse','featured','adjustment') AND l.delta>0 AND l.kind='award'`, user, Day(s.Now())).Scan(&out.Today); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,level,created_at FROM metar_exp_notice WHERE user_id=? AND read_at=0 ORDER BY id DESC LIMIT 8`, user)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Notice
			if err = rows.Scan(&n.ID, &n.Level, &n.CreatedAt); err != nil {
				return err
			}
			out.Notices = append(out.Notices, n)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) History(ctx context.Context, user string, before uint64) ([]Entry, error) {
	out := []Entry{}
	err := s.transaction(ctx, user, func(tx *sql.Tx, _ *Account) error {
		if before == 0 {
			before = math.MaxUint64
		}
		rows, err := tx.QueryContext(ctx, `SELECT l.id,l.delta,e.kind,l.balance,l.reason,l.created_at,e.object_type,e.object_id FROM metar_exp_ledger l JOIN metar_exp_event e ON e.id=l.event_id WHERE l.user_id=? AND l.id<? ORDER BY l.id DESC LIMIT 30`, user, before)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v Entry
			if err = rows.Scan(&v.ID, &v.Delta, &v.Kind, &v.Balance, &v.Reason, &v.CreatedAt, &v.ObjectType, &v.ObjectID); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) Pending(ctx context.Context, user string) ([]Event, error) {
	out := []Event{}
	err := s.transaction(ctx, user, func(tx *sql.Tx, _ *Account) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+eventColumns+` FROM metar_exp_event WHERE user_id=? AND status='pending' ORDER BY id DESC LIMIT 30`, user)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanEvent(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (s *Store) Appearance(ctx context.Context, user, value string) error {
	return s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		if !AppearanceAllowed(value, LevelFor(a.Balance).Number) {
			return ErrForbidden
		}
		_, err := tx.ExecContext(ctx, `UPDATE metar_exp_account SET appearance=? WHERE user_id=?`, value, user)
		return err
	})
}
func (s *Store) ReadNotices(ctx context.Context, user string) error {
	return s.transaction(ctx, user, func(tx *sql.Tx, _ *Account) error {
		_, err := tx.ExecContext(ctx, `UPDATE metar_exp_notice SET read_at=? WHERE user_id=? AND read_at=0`, s.Now().Unix(), user)
		return err
	})
}
func (s *Store) Public(ctx context.Context, username string) (map[string]any, error) {
	var balance int64
	var appearance string
	err := s.DB.QueryRowContext(ctx, "SELECT COALESCE(a.balance,0),COALESCE(a.appearance,'default') FROM `user` u LEFT JOIN metar_exp_account a ON a.user_id=u.id WHERE u.username=? AND u.status=1 AND u.mail_status=1", username).Scan(&balance, &appearance)
	if err != nil {
		return nil, err
	}
	level := LevelFor(balance)
	if !AppearanceAllowed(appearance, level.Number) {
		appearance = "default"
	}
	return map[string]any{"level": level, "appearance": appearance}, nil
}
func revoke(ctx context.Context, tx *sql.Tx, a *Account, e Event, reason string, now time.Time) error {
	if e.Status == "revoked" || e.Status == "capped" {
		return nil
	}
	if e.Status == "awarded" {
		if err := appendLedger(ctx, tx, a, e, -e.Amount, "reversal", reason, "0", now); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE metar_exp_event SET status='revoked' WHERE id=?`, e.ID)
	return err
}

// AwardPulse accepts only a server-verified Pulse grant, never browser amounts.
// Reversal tombstones prevent stale successful responses from resurrecting it.
func (s *Store) AwardPulse(ctx context.Context, user, grant string, amount int64, reversed bool) error {
	if len(grant) < 1 || len(grant) > 100 || strings.ContainsAny(grant, " /\\") {
		return ErrInvalid
	}
	return s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		now := s.Now()
		e, err := register(ctx, tx, Event{Source: "pulse:" + grant, UserID: user, Kind: "pulse", OccurredAt: now.Unix(), EligibleAt: now.Unix(), Day: Day(now), Amount: amount, RuleVersion: 1, Reason: "脉冲抽奖经验奖励"})
		if err != nil {
			return err
		}
		if reversed {
			return revoke(ctx, tx, a, e, "脉冲奖励已撤销", now)
		}
		return settle(ctx, tx, a, e, now)
	})
}
func textOK(s string, n int) bool { return len(strings.TrimSpace(s)) > 0 && len(s) <= n }
func (s *Store) CheckAdmin(ctx context.Context, user string) error {
	var count int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user` u JOIN user_role_rel r ON r.user_id=u.id WHERE u.id=? AND u.status=1 AND u.mail_status=1 AND r.role_id=2", user).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrForbidden
	}
	return nil
}
func audit(ctx context.Context, tx *sql.Tx, actor, key, operation, reason string, body any, now time.Time) (bool, error) {
	if !textOK(key, 96) || !textOK(reason, 500) {
		return false, ErrInvalid
	}
	fp := fingerprint(body)
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM metar_exp_audit WHERE request_key=?`, actor+":"+key).Scan(&existing)
	if err == nil {
		if existing != fp {
			return false, ErrConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	raw, _ := json.Marshal(body)
	_, err = tx.ExecContext(ctx, `INSERT INTO metar_exp_audit(request_key,fingerprint,actor_id,operation,reason,details,created_at) VALUES(?,?,?,?,?,?,?)`, actor+":"+key, fp, actor, operation, reason, string(raw), now.Unix())
	return false, err
}
func (s *Store) UpdateRules(ctx context.Context, actor, key, reason string, version int64, r Rules) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := s.CheckAdmin(ctx, actor); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT version FROM metar_exp_config WHERE id=1 FOR UPDATE`).Scan(&current); err != nil {
		return err
	}
	replay, err := audit(ctx, tx, actor, key, "rules", reason, []any{version, r, reason}, s.Now())
	if err != nil {
		return err
	}
	if replay {
		return tx.Commit()
	}
	if current != version {
		return ErrConflict
	}
	raw, _ := json.Marshal(r)
	if _, err = tx.ExecContext(ctx, `UPDATE metar_exp_config SET version=version+1,rules_json=? WHERE id=1`, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Adjust(ctx context.Context, actor, key, user, reason string, delta int64) error {
	if delta == 0 || delta > 100000 || delta < -100000 {
		return ErrInvalid
	}
	if err := s.CheckAdmin(ctx, actor); err != nil {
		return err
	}
	return s.transaction(ctx, user, func(tx *sql.Tx, a *Account) error {
		replay, err := audit(ctx, tx, actor, key, "adjustment", reason, []any{user, reason, delta}, s.Now())
		if err != nil || replay {
			return err
		}
		if delta < 0 && a.Balance+delta < 0 {
			return ErrInvalid
		}
		amount := delta
		if amount < 0 {
			amount = -amount
		}
		e, err := register(ctx, tx, Event{Source: fmt.Sprintf("admin:%s:%s", actor, key), UserID: user, Kind: "adjustment", ActorID: actor, OccurredAt: s.Now().Unix(), EligibleAt: s.Now().Unix(), Day: Day(s.Now()), Amount: amount, RuleVersion: 1, Reason: reason})
		if err != nil {
			return err
		}
		if err = appendLedger(ctx, tx, a, e, delta, "adjustment", reason, actor, s.Now()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE metar_exp_event SET status='awarded' WHERE id=?`, e.ID)
		return err
	})
}
