package chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func active(ctx context.Context, q queryer, user string) error {
	var status, mail int
	if !validID(user) {
		return ErrForbidden
	}
	err := q.QueryRowContext(ctx, "SELECT status,mail_status FROM `user` WHERE id=?", user).Scan(&status, &mail)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (status != 1 || mail != 1) {
		return ErrUnavailable
	}
	return err
}
func admin(ctx context.Context, q queryer, user string) error {
	if err := active(ctx, q, user); err != nil {
		return err
	}
	var roles, admins int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(role_id=2),0) FROM user_role_rel WHERE user_id=?`, user).Scan(&roles, &admins); err != nil {
		return err
	}
	if roles != 1 || admins != 1 {
		return ErrForbidden
	}
	return nil
}
func person(ctx context.Context, q queryer, user string) (Person, error) {
	var p Person
	err := q.QueryRowContext(ctx, "SELECT id,username,display_name,avatar FROM `user` WHERE id=?", user).Scan(&p.ID, &p.Username, &p.Name, &p.Avatar)
	return p, err
}
func contact(ctx context.Context, q queryer, a, b string) error {
	if a == b || !validID(b) {
		return ErrInvalid
	}
	if err := active(ctx, q, b); err != nil {
		return ErrBlocked
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_chat_block WHERE (user_id=? AND target_id=?) OR (user_id=? AND target_id=?)`, a, b, b, a).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrBlocked
	}
	return nil
}
func room(ctx context.Context, tx *sql.Tx, user, id, lock string) (Room, error) {
	var r Room
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.kind,r.title,r.owner_id,r.seq,r.closed,r.updated_at,r.version,m.state,m.since_seq,m.read_seq,m.unread_count FROM metar_chat_room r JOIN metar_chat_member m ON m.room_id=r.id WHERE r.id=? AND m.user_id=? `+lock, id, user).Scan(&r.ID, &r.Kind, &r.Title, &r.OwnerID, &r.Seq, &r.Closed, &r.UpdatedAt, &r.Version, &r.State, &r.Since, &r.Read, &r.Unread)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrForbidden
	}
	return r, err
}
func capacity(ctx context.Context, tx *sql.Tx, user string) error {
	var locked string
	if _, err := tx.ExecContext(ctx, `INSERT INTO metar_chat_limit(user_id) VALUES(?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)`, user); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT user_id FROM metar_chat_limit WHERE user_id=? FOR UPDATE`, user).Scan(&locked); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_chat_member WHERE user_id=? AND state IN ('active','invited')`, user).Scan(&n); err != nil {
		return err
	}
	if n >= 100 {
		return ErrLimit
	}
	return nil
}
func (s *Store) Execute(ctx context.Context, user, key string, c Command) (Result, error) {
	if !keyPattern.MatchString(key) || !validID(user) {
		return Result{}, ErrInvalid
	}
	raw, _ := json.Marshal(c)
	h := sha256.Sum256(raw)
	fp := hex.EncodeToString(h[:])
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.execute(ctx, user, key, fp, c)
		var e *mysql.MySQLError
		if !errors.As(err, &e) || e.Number != 1213 {
			return result, err
		}
	}
	return Result{}, ErrConflict
}
func (s *Store) execute(ctx context.Context, user, key, fp string, c Command) (Result, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	// One actor lock serializes rate limits and duplicate operations across instances.
	if _, err = tx.ExecContext(ctx, `INSERT INTO metar_chat_limit(user_id) VALUES(?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)`, user); err != nil {
		return Result{}, err
	}
	var minute, day int64
	var count, messages, creates, invites, reports int
	if err = tx.QueryRowContext(ctx, `SELECT minute_bucket,minute_count,day_bucket,message_count,create_count,invite_count,report_count FROM metar_chat_limit WHERE user_id=? FOR UPDATE`, user).Scan(&minute, &count, &day, &messages, &creates, &invites, &reports); err != nil {
		return Result{}, err
	}
	if err = active(ctx, tx, user); err != nil {
		return Result{}, err
	}
	if c.Op == "resolve_report" {
		if err = admin(ctx, tx, user); err != nil {
			return Result{}, err
		}
	}
	var previous, saved string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,response FROM metar_chat_request WHERE user_id=? AND request_key=?`, user, key).Scan(&previous, &saved)
	if err == nil {
		if fp != previous {
			return Result{}, ErrConflict
		}
		// Replays never disclose a room after membership was revoked.
		var out Result
		if err = json.Unmarshal([]byte(saved), &out); err != nil {
			return out, err
		}
		if out.RoomID != "" && c.Op != "leave" && c.Op != "decline" {
			r, e := room(ctx, tx, user, out.RoomID, "FOR SHARE")
			if e != nil || r.State == "left" {
				return Result{}, ErrForbidden
			}
		}
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Result{}, err
	}
	now := s.Now().Unix()
	if minute != now/60 {
		minute = now / 60
		count = 0
	}
	if day != now/86400 {
		day = now / 86400
		messages = 0
		creates = 0
		invites = 0
		reports = 0
	}
	if count >= 120 {
		return Result{}, ErrLimit
	}
	count++
	switch c.Op {
	case "send":
		if messages >= 1000 {
			return Result{}, ErrLimit
		}
		messages++
	case "direct", "group":
		if creates >= 20 {
			return Result{}, ErrLimit
		}
		creates++
	case "invite":
		if invites >= 100 {
			return Result{}, ErrLimit
		}
		invites++
	case "report":
		if reports >= 20 {
			return Result{}, ErrLimit
		}
		reports++
	}
	out, err := s.apply(ctx, tx, user, c, now)
	if err != nil {
		return Result{}, err
	}
	out.OK = true
	if _, err = tx.ExecContext(ctx, `UPDATE metar_chat_limit SET minute_bucket=?,minute_count=?,day_bucket=?,message_count=?,create_count=?,invite_count=?,report_count=? WHERE user_id=?`, minute, count, day, messages, creates, invites, reports, user); err != nil {
		return Result{}, err
	}
	data, _ := json.Marshal(out)
	if _, err = tx.ExecContext(ctx, `INSERT INTO metar_chat_request(user_id,request_key,fingerprint,response,created_at) VALUES(?,?,?,?,?)`, user, key, fp, string(data), now); err != nil {
		return Result{}, err
	}
	return out, tx.Commit()
}
func (s *Store) apply(ctx context.Context, tx *sql.Tx, user string, c Command, now int64) (Result, error) {
	out := Result{}
	if c.Op == "direct" || c.Op == "group" {
		return s.create(ctx, tx, user, c, now)
	}
	if c.Op == "block" || c.Op == "unblock" {
		if !validID(c.Target) || c.Target == user {
			return out, ErrInvalid
		}
		if c.Op == "block" {
			// The actor guard serializes this limit with concurrent block commands.
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_chat_block WHERE user_id=? AND target_id<>?`, user, c.Target).Scan(&count); err != nil {
				return out, err
			}
			if count >= 200 {
				return out, ErrLimit
			}
			var exists int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user` WHERE id=?", c.Target).Scan(&exists); err != nil {
				return out, err
			}
			if exists != 1 {
				return out, ErrInvalid
			}
			_, err := tx.ExecContext(ctx, `INSERT IGNORE INTO metar_chat_block(user_id,target_id) VALUES(?,?)`, user, c.Target)
			return out, err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM metar_chat_block WHERE user_id=? AND target_id=?`, user, c.Target)
		return out, err
	}
	if c.Op == "resolve_report" {
		if !validID(c.ReportID) || !validText(c.Reason, 500) || (c.Body != "dismiss" && c.Body != "remove") {
			return out, ErrInvalid
		}
		var roomID, status string
		var seq int64
		if err := tx.QueryRowContext(ctx, `SELECT room_id,seq,status FROM metar_chat_report WHERE id=? FOR UPDATE`, c.ReportID).Scan(&roomID, &seq, &status); err != nil {
			return out, err
		}
		if status != "pending" {
			return out, ErrConflict
		}
		if c.Body == "remove" {
			if err := redact(ctx, tx, roomID, seq); err != nil {
				return out, err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE metar_chat_report SET status=?,resolution=?,reviewer_id=?,reviewed_at=? WHERE id=?`, c.Body, c.Reason, user, now, c.ReportID)
		return out, err
	}
	if !validID(c.RoomID) {
		return out, ErrInvalid
	}
	r, err := room(ctx, tx, user, c.RoomID, "FOR UPDATE")
	if err != nil {
		return out, err
	}
	out.RoomID = r.ID
	if c.Op == "accept" || c.Op == "decline" {
		if r.State != "invited" || r.Closed {
			return out, ErrForbidden
		}
		if c.Op == "decline" {
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET state='left' WHERE room_id=? AND user_id=?`, r.ID, user)
			return out, err
		}
		if err = contact(ctx, tx, user, r.OwnerID); err != nil {
			return out, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET state='active',since_seq=?,read_seq=?,unread_count=0 WHERE room_id=? AND user_id=?`, r.Seq+1, r.Seq, r.ID, user)
		return out, err
	}
	if r.State != "active" {
		return out, ErrForbidden
	}
	switch c.Op {
	case "read":
		if c.Seq < 0 || c.Seq > r.Seq {
			return out, ErrInvalid
		}
		if c.Seq > r.Read {
			var unread int64
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_chat_message WHERE room_id=? AND seq>? AND sender_id<>?`, r.ID, c.Seq, user).Scan(&unread); err != nil {
				return out, err
			}
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET read_seq=?,unread_count=? WHERE room_id=? AND user_id=?`, c.Seq, unread, r.ID, user)
		}
	case "send":
		if r.Closed || !validText(c.Body, 4000) {
			return out, ErrInvalid
		}
		if r.Kind == "direct" {
			var peer string
			if err = tx.QueryRowContext(ctx, `SELECT user_id FROM metar_chat_member WHERE room_id=? AND user_id<>?`, r.ID, user).Scan(&peer); err != nil {
				return out, err
			}
			if err = contact(ctx, tx, user, peer); err != nil {
				return out, err
			}
		}
		out.Seq = r.Seq + 1
		if _, err = tx.ExecContext(ctx, `INSERT INTO metar_chat_message(room_id,seq,sender_id,body,created_at,version) VALUES(?,?,?,?,?,?)`, r.ID, out.Seq, user, strings.TrimSpace(c.Body), now, r.Version+1); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE metar_chat_room SET seq=?,updated_at=?,version=version+1 WHERE id=?`, out.Seq, now, r.ID); err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET unread_count=unread_count+1 WHERE room_id=? AND user_id<>? AND state='active'`, r.ID, user); err != nil {
			return out, err
		}
		// Sending does not mark messages the sender has not displayed as read.
	case "recall":
		var author string
		var created int64
		if err = tx.QueryRowContext(ctx, `SELECT sender_id,created_at FROM metar_chat_message WHERE room_id=? AND seq=?`, r.ID, c.Seq).Scan(&author, &created); err != nil {
			return out, err
		}
		if author != user || now-created > 120 || c.Seq < r.Since {
			return out, ErrForbidden
		}
		err = redact(ctx, tx, r.ID, c.Seq)
	case "report":
		if !validText(c.Reason, 500) || c.Seq < r.Since {
			return out, ErrInvalid
		}
		var author, body string
		var removed bool
		if err = tx.QueryRowContext(ctx, `SELECT sender_id,body,removed FROM metar_chat_message WHERE room_id=? AND seq=?`, r.ID, c.Seq).Scan(&author, &body, &removed); err != nil {
			return out, err
		}
		if removed || author == user {
			return out, ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO metar_chat_report(room_id,seq,reporter_id,sender_id,body,reason,created_at) VALUES(?,?,?,?,?,?,?)`, r.ID, c.Seq, user, author, body, c.Reason, now)
	case "rename", "invite", "remove", "transfer", "close", "leave":
		if r.Kind != "group" || r.Closed {
			return out, ErrForbidden
		}
		if c.Op != "leave" && r.OwnerID != user {
			return out, ErrForbidden
		}
		switch c.Op {
		case "rename":
			if !validText(c.Title, 80) {
				return out, ErrInvalid
			}
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_room SET title=? WHERE id=?`, strings.TrimSpace(c.Title), r.ID)
		case "invite":
			err = s.invite(ctx, tx, user, r, c.Target)
		case "remove":
			if c.Target == user || !validID(c.Target) {
				return out, ErrInvalid
			}
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET state='left' WHERE room_id=? AND user_id=?`, r.ID, c.Target)
		case "transfer":
			var state string
			if err = tx.QueryRowContext(ctx, `SELECT state FROM metar_chat_member WHERE room_id=? AND user_id=?`, r.ID, c.Target).Scan(&state); err != nil {
				return out, ErrInvalid
			}
			if state != "active" || c.Target == user {
				return out, ErrInvalid
			}
			if err = active(ctx, tx, c.Target); err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE metar_chat_room SET owner_id=? WHERE id=?`, c.Target, r.ID)
			}
		case "close":
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_room SET closed=1 WHERE id=?`, r.ID)
		case "leave":
			if r.OwnerID == user {
				return out, ErrConflict
			}
			_, err = tx.ExecContext(ctx, `UPDATE metar_chat_member SET state='left' WHERE room_id=? AND user_id=?`, r.ID, user)
		}
	default:
		return out, ErrInvalid
	}
	return out, err
}
func (s *Store) invite(ctx context.Context, tx *sql.Tx, user string, r Room, target string) error {
	if err := contact(ctx, tx, user, target); err != nil {
		return err
	}
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM metar_chat_member WHERE room_id=? AND user_id=?`, r.ID, target).Scan(&state)
	if err == nil && (state == "active" || state == "invited") {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM metar_chat_member WHERE room_id=? AND state IN ('active','invited')`, r.ID).Scan(&count); err != nil {
		return err
	}
	if count >= MaxMembers {
		return ErrLimit
	}
	if err = capacity(ctx, tx, target); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO metar_chat_member(room_id,user_id,state,since_seq,read_seq,invited_by) VALUES(?,?,'invited',?,?,?) ON DUPLICATE KEY UPDATE state='invited',since_seq=VALUES(since_seq),read_seq=VALUES(read_seq),unread_count=0,invited_by=VALUES(invited_by)`, r.ID, target, r.Seq+1, r.Seq, user)
	return err
}
func (s *Store) create(ctx context.Context, tx *sql.Tx, user string, c Command, now int64) (Result, error) {
	out := Result{}
	kind := "group"
	var pair any
	if c.Op == "direct" {
		if err := contact(ctx, tx, user, c.Target); err != nil {
			return out, err
		}
		kind = "direct"
		ids := []string{user, c.Target}
		sort.Strings(ids)
		pair = strings.Join(ids, ":")
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM metar_chat_room WHERE pair_key=?`, pair).Scan(&existing)
		if err == nil {
			return Result{RoomID: existing}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if err = capacity(ctx, tx, c.Target); err != nil {
			return out, err
		}
	} else if !validText(c.Title, 80) || len(c.Members) > MaxMembers-1 {
		return out, ErrInvalid
	}
	if err := capacity(ctx, tx, user); err != nil {
		return out, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO metar_chat_room(kind,pair_key,title,owner_id,updated_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, kind, pair, strings.TrimSpace(c.Title), user, now)
	if err != nil {
		return out, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return out, err
	}
	out.RoomID = strconv.FormatInt(id, 10)
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO metar_chat_member(room_id,user_id,state) VALUES(?,?,'active')`, out.RoomID, user); err != nil {
		return out, err
	}
	if kind == "direct" {
		_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO metar_chat_member(room_id,user_id,state) VALUES(?,?,'active')`, out.RoomID, c.Target)
		return out, err
	}
	seen := map[string]bool{user: true}
	for _, target := range c.Members {
		if seen[target] {
			return out, ErrInvalid
		}
		seen[target] = true
		if err = s.invite(ctx, tx, user, Room{ID: out.RoomID}, target); err != nil {
			return out, err
		}
	}
	return out, nil
}

func redact(ctx context.Context, tx *sql.Tx, id string, seq int64) error {
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM metar_chat_room WHERE id=? FOR UPDATE`, id).Scan(&version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE metar_chat_message SET body='',removed=1,version=? WHERE room_id=? AND seq=?`, version+1, id, seq); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE metar_chat_room SET version=version+1 WHERE id=?`, id)
	return err
}
