// Package events owns free community announcements and registration in Answer.
package events

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid_request")
var ErrForbidden = errors.New("forbidden")
var ErrConflict = errors.New("conflict")
var ErrFull = errors.New("event_full")
var ErrClosed = errors.New("event_closed")
var ErrLimit = errors.New("rate_limited")
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,96}$`)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{DB: db, Now: time.Now} }

type Event struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Location string `json:"location"`
	Starts   int64  `json:"starts_at"`
	Ends     int64  `json:"ends_at"`
	Deadline int64  `json:"deadline"`
	Capacity int    `json:"capacity"`
	Count    int    `json:"registered_count"`
	Status   string `json:"status"`
	Version  int    `json:"version"`
	Joined   bool   `json:"joined"`
}
type Command struct {
	Op    string `json:"op"`
	Event Event  `json:"event"`
}
type Result struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}
type Page struct {
	Items []Event `json:"items"`
	More  bool    `json:"more"`
}
type Person struct {
	Username string `json:"username"`
	Name     string `json:"display_name"`
	JoinedAt int64  `json:"joined_at"`
}

func (s *Store) Ensure(ctx context.Context) error {
	c, e := s.DB.Conn(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	var locked int
	if e = c.QueryRowContext(ctx, `SELECT GET_LOCK('metar_events_v1',10)`).Scan(&locked); e != nil {
		return e
	}
	if locked != 1 {
		return errors.New("event schema busy")
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.ExecContext(ctx, `SELECT RELEASE_LOCK('metar_events_v1')`)
	}()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS metar_event(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,title VARCHAR(100) NOT NULL,body TEXT NOT NULL,location VARCHAR(300) NOT NULL,starts_at BIGINT NOT NULL,ends_at BIGINT NOT NULL,deadline BIGINT NOT NULL,capacity INT NOT NULL,registered_count INT NOT NULL DEFAULT 0,status VARCHAR(16) NOT NULL,version INT NOT NULL DEFAULT 1,created_by BIGINT UNSIGNED NOT NULL,updated_by BIGINT UNSIGNED NOT NULL,INDEX upcoming(starts_at,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS metar_event_member(event_id BIGINT UNSIGNED NOT NULL,user_id BIGINT UNSIGNED NOT NULL,joined BOOLEAN NOT NULL,joined_at BIGINT NOT NULL,PRIMARY KEY(event_id,user_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_event_actor(user_id BIGINT UNSIGNED PRIMARY KEY,minute_bucket BIGINT NOT NULL DEFAULT 0,command_count INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_event_request(user_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,fingerprint CHAR(64) NOT NULL,response VARCHAR(200) NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(user_id,request_key)) ENGINE=InnoDB`,
	} {
		if _, e = c.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func active(ctx context.Context, q queryer, user string, admin bool) error {
	n, e := strconv.ParseUint(user, 10, 64)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != user {
		return ErrForbidden
	}
	var status, mail int
	if e = q.QueryRowContext(ctx, "SELECT status,mail_status FROM `user` WHERE id=?", user).Scan(&status, &mail); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return ErrForbidden
		}
		return e
	}
	if status != 1 || mail != 1 {
		return ErrForbidden
	}
	if admin {
		var count, admins int
		if e = q.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(role_id=2),0) FROM user_role_rel WHERE user_id=?`, user).Scan(&count, &admins); e != nil {
			return e
		}
		if count != 1 || admins != 1 {
			return ErrForbidden
		}
	}
	return nil
}
func valid(v Event, now int64) bool {
	return strings.TrimSpace(v.Title) != "" && len([]rune(v.Title)) <= 100 && strings.TrimSpace(v.Body) != "" && len(v.Body) <= 12000 && len([]rune(v.Location)) <= 300 && v.Starts > now && v.Ends > v.Starts && v.Ends-v.Starts <= 86400*90 && v.Deadline > now && v.Deadline <= v.Starts && v.Capacity >= 1 && v.Capacity <= 100000
}
func (s *Store) Execute(ctx context.Context, user, key string, c Command) (Result, error) {
	for attempt := 0; attempt < 4; attempt++ {
		r, e := s.execute(ctx, user, key, c)
		var dbErr *mysql.MySQLError
		if !errors.As(e, &dbErr) || dbErr.Number != 1213 {
			return r, e
		}
	}
	return Result{}, ErrConflict
}
func (s *Store) execute(ctx context.Context, user, key string, c Command) (Result, error) {
	if !keyPattern.MatchString(key) {
		return Result{}, ErrInvalid
	}
	admin := c.Op == "create" || c.Op == "edit" || c.Op == "cancel"
	if !admin && c.Op != "join" && c.Op != "leave" {
		return Result{}, ErrInvalid
	}
	if e := active(ctx, s.DB, user, admin); e != nil {
		return Result{}, e
	}
	// Persist the lock row independently so rejected full/closed requests cannot
	// repeatedly roll it back and contend on the same empty index gap.
	if _, e := s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_event_actor(user_id) VALUES(?)`, user); e != nil {
		return Result{}, e
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return Result{}, e
	}
	defer tx.Rollback()
	var actor string
	if e = tx.QueryRowContext(ctx, `SELECT user_id FROM metar_event_actor WHERE user_id=? FOR UPDATE`, user).Scan(&actor); e != nil {
		return Result{}, e
	}
	if e = active(ctx, tx, user, admin); e != nil {
		return Result{}, e
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	fp := hex.EncodeToString(sum[:])
	now := s.Now().Unix()
	if _, e = tx.ExecContext(ctx, `INSERT INTO metar_event_request(user_id,request_key,fingerprint,response,created_at) VALUES(?,?,?,'',?) ON DUPLICATE KEY UPDATE request_key=VALUES(request_key)`, user, key, fp, now); e != nil {
		return Result{}, e
	}
	var savedFP, saved string
	if e = tx.QueryRowContext(ctx, `SELECT fingerprint,response FROM metar_event_request WHERE user_id=? AND request_key=? FOR UPDATE`, user, key).Scan(&savedFP, &saved); e != nil {
		return Result{}, e
	}
	if savedFP != fp {
		return Result{}, ErrConflict
	}
	if saved != "" {
		var r Result
		if e = json.Unmarshal([]byte(saved), &r); e != nil {
			return r, e
		}
		return r, tx.Commit()
	}
	var minute int64
	var commands int
	if e = tx.QueryRowContext(ctx, `SELECT minute_bucket,command_count FROM metar_event_actor WHERE user_id=? FOR UPDATE`, user).Scan(&minute, &commands); e != nil {
		return Result{}, e
	}
	if minute != now/60 {
		commands = 0
	}
	if commands >= 60 {
		return Result{}, ErrLimit
	}
	if _, e = tx.ExecContext(ctx, `UPDATE metar_event_actor SET minute_bucket=?,command_count=? WHERE user_id=?`, now/60, commands+1, user); e != nil {
		return Result{}, e
	}
	v := c.Event
	r := Result{ID: v.ID}
	if c.Op == "create" {
		if v.ID != "" || !valid(v, now) {
			return r, ErrInvalid
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO metar_event(title,body,location,starts_at,ends_at,deadline,capacity,status,created_by,updated_by) VALUES(?,?,?,?,?,?,?,'published',?,?)`, v.Title, v.Body, v.Location, v.Starts, v.Ends, v.Deadline, v.Capacity, user, user)
		if e != nil {
			return r, e
		}
		id, _ := res.LastInsertId()
		r.ID = strconv.FormatInt(id, 10)
		r.Version = 1
	} else {
		id, e := strconv.ParseUint(v.ID, 10, 64)
		if e != nil || id == 0 || strconv.FormatUint(id, 10) != v.ID {
			return r, ErrInvalid
		}
		var status string
		var count, capacity, version int
		var start, deadline int64
		e = tx.QueryRowContext(ctx, `SELECT status,registered_count,capacity,version,starts_at,deadline FROM metar_event WHERE id=? FOR UPDATE`, v.ID).Scan(&status, &count, &capacity, &version, &start, &deadline)
		if errors.Is(e, sql.ErrNoRows) {
			return r, ErrInvalid
		}
		if e != nil {
			return r, e
		}
		r.Version = version
		if admin {
			if v.Version != version {
				return r, ErrConflict
			}
			if status != "published" {
				return r, ErrClosed
			}
			if c.Op == "cancel" {
				_, e = tx.ExecContext(ctx, `UPDATE metar_event SET status='cancelled',version=version+1,updated_by=? WHERE id=?`, user, v.ID)
			} else {
				if start <= now || !valid(v, now) || v.Capacity < count {
					return r, ErrInvalid
				}
				_, e = tx.ExecContext(ctx, `UPDATE metar_event SET title=?,body=?,location=?,starts_at=?,ends_at=?,deadline=?,capacity=?,version=version+1,updated_by=? WHERE id=?`, v.Title, v.Body, v.Location, v.Starts, v.Ends, v.Deadline, v.Capacity, user, v.ID)
			}
			r.Version++
		} else {
			var joined bool
			e = tx.QueryRowContext(ctx, `SELECT joined FROM metar_event_member WHERE event_id=? AND user_id=?`, v.ID, user).Scan(&joined)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return r, e
			}
			e = nil
			desired := c.Op == "join"
			if desired != joined {
				if desired && (status != "published" || now >= deadline) {
					return r, ErrClosed
				}
				if desired && count >= capacity {
					return r, ErrFull
				}
				delta := -1
				if desired {
					delta = 1
				}
				_, e = tx.ExecContext(ctx, `INSERT INTO metar_event_member(event_id,user_id,joined,joined_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE joined=VALUES(joined),joined_at=VALUES(joined_at)`, v.ID, user, desired, now)
				if e == nil {
					_, e = tx.ExecContext(ctx, `UPDATE metar_event SET registered_count=registered_count+? WHERE id=?`, delta, v.ID)
				}
			}
		}
		if e != nil {
			return r, e
		}
	}
	b, _ := json.Marshal(r)
	if _, e = tx.ExecContext(ctx, `UPDATE metar_event_request SET response=? WHERE user_id=? AND request_key=?`, string(b), user, key); e != nil {
		return r, e
	}
	return r, tx.Commit()
}
func (s *Store) List(ctx context.Context, user string, page int, past bool) (Page, error) {
	out := Page{Items: []Event{}}
	if page < 1 || page > 1000 {
		return out, ErrInvalid
	}
	if user != "" {
		if e := active(ctx, s.DB, user, false); e != nil {
			return out, e
		}
	}
	op := ">="
	order := "ASC"
	if past {
		op = "<"
		order = "DESC"
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT e.id,e.title,e.body,e.location,e.starts_at,e.ends_at,e.deadline,e.capacity,e.registered_count,e.status,e.version,COALESCE(m.joined,0) FROM metar_event e LEFT JOIN metar_event_member m ON m.event_id=e.id AND m.user_id=? WHERE e.ends_at `+op+` ? ORDER BY e.starts_at `+order+`,e.id `+order+` LIMIT 21 OFFSET ?`, user, s.Now().Unix(), (page-1)*20)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.Title, &v.Body, &v.Location, &v.Starts, &v.Ends, &v.Deadline, &v.Capacity, &v.Count, &v.Status, &v.Version, &v.Joined); e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > 20 {
		out.More = true
		out.Items = out.Items[:20]
	}
	return out, rows.Err()
}
func (s *Store) Roster(ctx context.Context, user, id string, page int) ([]Person, error) {
	if e := active(ctx, s.DB, user, true); e != nil {
		return nil, e
	}
	n, e := strconv.ParseUint(id, 10, 64)
	if e != nil || n == 0 || page < 1 || page > 1000 {
		return nil, ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT u.username,u.display_name,m.joined_at FROM metar_event_member m JOIN `user` u ON u.id=m.user_id WHERE m.event_id=? AND m.joined=1 ORDER BY m.user_id LIMIT 100 OFFSET ?", id, (page-1)*100)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var p Person
		if e = rows.Scan(&p.Username, &p.Name, &p.JoinedAt); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
