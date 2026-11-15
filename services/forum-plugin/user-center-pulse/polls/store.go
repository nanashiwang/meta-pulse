// Package polls owns optional topic polls, never reputation or financial rewards.
package polls

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid_request")
var ErrForbidden = errors.New("forbidden")
var ErrNotFound = errors.New("not_found")
var ErrConflict = errors.New("conflict")
var ErrClosed = errors.New("poll_closed")
var idPattern = regexp.MustCompile("^[1-9][0-9]{0,19}$")
var keyPattern = regexp.MustCompile("^[a-zA-Z0-9_-]{16,80}$")

type Config struct {
	Title      string   `json:"title"`
	Options    []string `json:"options"`
	MaxChoices int      `json:"max_choices"`
	Days       int      `json:"days"`
}
type Command struct {
	Op         string  `json:"op"`
	QuestionID string  `json:"question_id"`
	Config     *Config `json:"config,omitempty"`
	Choices    []int   `json:"choices,omitempty"`
	Version    int64   `json:"version"`
}
type View struct {
	Exists         bool     `json:"exists"`
	QuestionID     string   `json:"question_id"`
	Title          string   `json:"title,omitempty"`
	Options        []string `json:"options,omitempty"`
	MaxChoices     int      `json:"max_choices,omitempty"`
	ClosesAt       int64    `json:"closes_at,omitempty"`
	Ended          bool     `json:"ended"`
	EarlyClosed    bool     `json:"early_closed"`
	CanCreate      bool     `json:"can_create"`
	CanManage      bool     `json:"can_manage"`
	CanVote        bool     `json:"can_vote"`
	Selected       []int    `json:"selected"`
	Version        int64    `json:"version"`
	ResultsVisible bool     `json:"results_visible"`
	Total          *int64   `json:"total,omitempty"`
	Counts         []int64  `json:"counts,omitempty"`
}
type Receipt struct {
	OK bool `json:"ok"`
}
type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{DB: db, Now: time.Now} }
func (s *Store) Ensure(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS metar_topic_poll (question_id BIGINT UNSIGNED PRIMARY KEY, owner_id BIGINT UNSIGNED NOT NULL, title VARCHAR(160) NOT NULL, options_json TEXT NOT NULL, max_choices INT NOT NULL, closes_at BIGINT NOT NULL, closed_at BIGINT NOT NULL DEFAULT 0, created_at BIGINT NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_topic_poll_ballot (question_id BIGINT UNSIGNED NOT NULL,user_id BIGINT UNSIGNED NOT NULL,choices_json VARCHAR(128) NOT NULL,version BIGINT NOT NULL,updated_at BIGINT NOT NULL,PRIMARY KEY(question_id,user_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_topic_poll_command (user_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(80) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,fingerprint CHAR(64) NOT NULL,question_id BIGINT UNSIGNED NOT NULL,op VARCHAR(12) NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(user_id,request_key),KEY poll_commands(question_id,created_at)) ENGINE=InnoDB`,
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
func Normalize(c Command) (Command, error) {
	if !idPattern.MatchString(c.QuestionID) || c.Version < 0 {
		return c, ErrInvalid
	}
	switch c.Op {
	case "create":
		if c.Config == nil || len(c.Choices) > 0 || c.Version != 0 {
			return c, ErrInvalid
		}
		cfg := *c.Config
		cfg.Title = strings.TrimSpace(cfg.Title)
		cfg.Options = append([]string(nil), cfg.Options...)
		if !validText(cfg.Title, 160) || len(cfg.Options) < 2 || len(cfg.Options) > 10 || cfg.MaxChoices < 1 || cfg.MaxChoices > len(cfg.Options) {
			return c, ErrInvalid
		}
		if cfg.Days != 1 && cfg.Days != 3 && cfg.Days != 7 && cfg.Days != 30 {
			return c, ErrInvalid
		}
		seen := map[string]bool{}
		for i, value := range cfg.Options {
			value = strings.TrimSpace(value)
			if !validText(value, 100) || seen[strings.ToLower(value)] {
				return c, ErrInvalid
			}
			seen[strings.ToLower(value)] = true
			cfg.Options[i] = value
		}
		c.Config = &cfg
	case "vote":
		if c.Config != nil || len(c.Choices) < 1 || len(c.Choices) > 10 {
			return c, ErrInvalid
		}
		c.Choices = append([]int(nil), c.Choices...)
		sort.Ints(c.Choices)
		for i, v := range c.Choices {
			if v < 0 || v > 9 || (i > 0 && c.Choices[i-1] == v) {
				return c, ErrInvalid
			}
		}
	case "close":
		if c.Config != nil || len(c.Choices) > 0 || c.Version != 0 {
			return c, ErrInvalid
		}
	default:
		return c, ErrInvalid
	}
	return c, nil
}
func validText(v string, max int) bool {
	if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func active(ctx context.Context, tx *sql.Tx, user string) error {
	if !idPattern.MatchString(user) {
		return ErrForbidden
	}
	var status, mail int
	if err := tx.QueryRowContext(ctx, "SELECT status,mail_status FROM `user` WHERE id=? FOR SHARE", user).Scan(&status, &mail); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		return err
	}
	if status != 1 || mail != 1 {
		return ErrForbidden
	}
	return nil
}
func question(ctx context.Context, tx *sql.Tx, id, user string, create, write bool) (string, int, error) {
	var owner string
	var status, show int
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	err := tx.QueryRowContext(ctx, "SELECT user_id,status,`show` FROM question WHERE id=?"+lock, id).Scan(&owner, &status, &show)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, err
	}
	if create && owner == user && (status == 1 || status == 11) {
		return owner, status, nil
	}
	if (status != 1 && status != 2) || show != 1 {
		return "", 0, ErrNotFound
	}
	return owner, status, nil
}

func (s *Store) Execute(ctx context.Context, user, key string, c Command) (Receipt, error) {
	if !keyPattern.MatchString(key) {
		return Receipt{}, ErrInvalid
	}
	c, err := Normalize(c)
	if err != nil {
		return Receipt{}, err
	}
	encoded, _ := json.Marshal(c)
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	if err = active(ctx, tx, user); err != nil {
		return Receipt{}, err
	}
	owner, status, err := question(ctx, tx, c.QuestionID, user, c.Op == "create", true)
	if err != nil {
		return Receipt{}, err
	}
	var prior string
	err = tx.QueryRowContext(ctx, "SELECT fingerprint FROM metar_topic_poll_command WHERE user_id=? AND request_key=?", user, key).Scan(&prior)
	if err == nil {
		if prior != fingerprint {
			return Receipt{}, ErrConflict
		}
		return Receipt{true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	now := s.Now().Unix()
	switch c.Op {
	case "create":
		if owner != user {
			return Receipt{}, ErrForbidden
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_topic_poll WHERE question_id=?", c.QuestionID).Scan(&count); err != nil {
			return Receipt{}, err
		}
		if count > 0 {
			return Receipt{}, ErrConflict
		}
		options, _ := json.Marshal(c.Config.Options)
		_, err = tx.ExecContext(ctx, "INSERT INTO metar_topic_poll(question_id,owner_id,title,options_json,max_choices,closes_at,created_at) VALUES(?,?,?,?,?,?,?)", c.QuestionID, user, c.Config.Title, string(options), c.Config.MaxChoices, now+int64(c.Config.Days)*86400, now)
	case "vote", "close":
		var options string
		var max int
		var closes, closed int64
		err = tx.QueryRowContext(ctx, "SELECT options_json,max_choices,closes_at,closed_at FROM metar_topic_poll WHERE question_id=? FOR UPDATE", c.QuestionID).Scan(&options, &max, &closes, &closed)
		if errors.Is(err, sql.ErrNoRows) {
			return Receipt{}, ErrNotFound
		}
		if err != nil {
			return Receipt{}, err
		}
		if c.Op == "close" {
			if owner != user {
				return Receipt{}, ErrForbidden
			}
			if closed == 0 && now < closes {
				_, err = tx.ExecContext(ctx, "UPDATE metar_topic_poll SET closed_at=? WHERE question_id=?", now, c.QuestionID)
			}
		} else {
			if status != 1 || closed > 0 || now >= closes {
				return Receipt{}, ErrClosed
			}
			var labels []string
			if err = json.Unmarshal([]byte(options), &labels); err != nil {
				return Receipt{}, err
			}
			if len(c.Choices) > max || c.Choices[len(c.Choices)-1] >= len(labels) {
				return Receipt{}, ErrInvalid
			}
			var version int64
			err = tx.QueryRowContext(ctx, "SELECT version FROM metar_topic_poll_ballot WHERE question_id=? AND user_id=?", c.QuestionID, user).Scan(&version)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return Receipt{}, err
			}
			if c.Version != version {
				return Receipt{}, ErrConflict
			}
			choices, _ := json.Marshal(c.Choices)
			_, err = tx.ExecContext(ctx, "INSERT INTO metar_topic_poll_ballot(question_id,user_id,choices_json,version,updated_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE choices_json=VALUES(choices_json),version=VALUES(version),updated_at=VALUES(updated_at)", c.QuestionID, user, string(choices), version+1, now)
		}
	}
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO metar_topic_poll_command(user_id,request_key,fingerprint,question_id,op,created_at) VALUES(?,?,?,?,?,?)", user, key, fingerprint, c.QuestionID, c.Op, now); err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return Receipt{true}, nil
}
func (s *Store) Get(ctx context.Context, id, user string) (View, error) {
	out := View{QuestionID: id, Selected: []int{}}
	if !idPattern.MatchString(id) {
		return out, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if user != "" {
		if err = active(ctx, tx, user); err != nil {
			return out, err
		}
	}
	owner, status, err := question(ctx, tx, id, user, false, false)
	if err != nil {
		return out, err
	}
	var options string
	var closed int64
	err = tx.QueryRowContext(ctx, "SELECT title,options_json,max_choices,closes_at,closed_at FROM metar_topic_poll WHERE question_id=?", id).Scan(&out.Title, &options, &out.MaxChoices, &out.ClosesAt, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		out.CanCreate = user == owner && status == 1
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Exists = true
	out.CanManage = user == owner
	out.EarlyClosed = closed > 0
	out.Ended = closed > 0 || s.Now().Unix() >= out.ClosesAt || status != 1
	out.CanVote = user != "" && !out.Ended
	if err = json.Unmarshal([]byte(options), &out.Options); err != nil {
		return out, err
	}
	if user != "" {
		var selected string
		err = tx.QueryRowContext(ctx, "SELECT choices_json,version FROM metar_topic_poll_ballot WHERE question_id=? AND user_id=?", id, user).Scan(&selected, &out.Version)
		if err == nil {
			if err = json.Unmarshal([]byte(selected), &out.Selected); err != nil {
				return out, err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	out.ResultsVisible = out.Ended || out.Version > 0
	if out.ResultsVisible {
		out.Counts = make([]int64, len(out.Options))
		var total int64
		rows, e := tx.QueryContext(ctx, "SELECT choice_rows.option_id,COUNT(*) FROM metar_topic_poll_ballot b CROSS JOIN JSON_TABLE(b.choices_json, '$[*]' COLUMNS(option_id INT PATH '$')) AS choice_rows WHERE b.question_id=? GROUP BY choice_rows.option_id", id)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var choice int
			var count int64
			if err = rows.Scan(&choice, &count); err != nil {
				rows.Close()
				return out, err
			}
			if choice >= 0 && choice < len(out.Counts) {
				out.Counts[choice] = count
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_topic_poll_ballot WHERE question_id=?", id).Scan(&total); err != nil {
			return out, err
		}
		out.Total = &total
	}
	return out, nil
}
