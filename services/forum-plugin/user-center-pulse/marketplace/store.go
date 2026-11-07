package marketplace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{DB: db, Now: time.Now} }

var schema = []string{
	`CREATE TABLE IF NOT EXISTS metar_market_item(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,question_id BIGINT UNSIGNED NOT NULL UNIQUE,author_id BIGINT UNSIGNED NOT NULL,status VARCHAR(24) NOT NULL,entry_json TEXT NOT NULL,live_json TEXT NULL,review_note VARCHAR(500) NOT NULL DEFAULT '',embed_allowed BOOLEAN NOT NULL DEFAULT FALSE,demo_status VARCHAR(16) NOT NULL DEFAULT 'available',featured BOOLEAN NOT NULL DEFAULT FALSE,version BIGINT NOT NULL DEFAULT 1,created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,published_at BIGINT NOT NULL DEFAULT 0,INDEX owner(author_id,id),INDEX published(published_at,id),INDEX queue(status,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS metar_market_actor(user_id BIGINT UNSIGNED PRIMARY KEY,minute_bucket BIGINT NOT NULL DEFAULT 0,commands INT NOT NULL DEFAULT 0,day_bucket BIGINT NOT NULL DEFAULT 0,creates INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_market_request(actor_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,response TEXT NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(actor_id,request_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS metar_market_audit(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,actor_id BIGINT UNSIGNED NOT NULL,item_id BIGINT UNSIGNED NOT NULL,op VARCHAR(24) NOT NULL,version BIGINT NOT NULL,reason VARCHAR(500) NOT NULL,before_json TEXT NOT NULL,after_json TEXT NOT NULL,created_at BIGINT NOT NULL,INDEX item_history(item_id,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func (s *Store) Ensure(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('metar_market_schema_v1',5)`).Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return errors.New("market schema busy")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK('metar_market_schema_v1')`)
	}()
	for _, q := range schema {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func active(ctx context.Context, q querier, user string, admin bool) error {
	if !validID(user) {
		return ErrForbidden
	}
	var status, mail, roles int
	err := q.QueryRowContext(ctx, "SELECT u.status,u.mail_status,(SELECT COUNT(*) FROM user_role_rel r WHERE r.user_id=u.id AND r.role_id=2) FROM `user` u WHERE u.id=?", user).Scan(&status, &mail, &roles)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if status != 1 || mail != 1 || (admin && roles < 1) {
		return ErrForbidden
	}
	return nil
}
func (s *Store) CheckAdmin(ctx context.Context, user string) error {
	return active(ctx, s.DB, user, true)
}
func validatePost(ctx context.Context, q querier, e Entry, user string, public bool) error {
	var owner string
	var status, show int
	err := q.QueryRowContext(ctx, "SELECT user_id,status,`show` FROM question WHERE id=?", e.QuestionID).Scan(&owner, &status, &show)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if owner != user || (status != 1 && status != 2) || (public && show != 1) {
		return ErrForbidden
	}
	if e.SourceIdeaID != "" {
		var kind string
		err = q.QueryRowContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(m.live_json,'$.kind'))"+publicFrom+"WHERE "+visible+" AND m.id=?", e.SourceIdeaID).Scan(&kind)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalid
		}
		if err != nil {
			return err
		}
		if kind != "idea" {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Store) Execute(ctx context.Context, user, key string, c Command, admin bool) (Item, error) {
	if !keyPattern.MatchString(key) || !validID(user) {
		return Item{}, ErrInvalid
	}
	if admin != (c.Op == "review") {
		return Item{}, ErrForbidden
	}
	switch c.Op {
	case "save_draft", "submit":
		if c.Decision != "" || c.Reason != "" || c.EmbedAllowed || c.DemoStatus != "" || c.Featured {
			return Item{}, ErrInvalid
		}
		var err error
		c.Entry, err = ValidateEntry(c.Entry, c.Op == "submit")
		if err != nil {
			return Item{}, err
		}
	case "archive":
		if !validID(c.ID) || c.Version < 1 || c.Entry != (Entry{}) || c.Decision != "" || c.EmbedAllowed || c.DemoStatus != "" || c.Featured || c.Reason != "" {
			return Item{}, ErrInvalid
		}
	case "review":
		c.Reason = strings.TrimSpace(c.Reason)
		if !validID(c.ID) || c.Version < 1 || c.Entry != (Entry{}) || len([]rune(c.Reason)) < 2 || len([]rune(c.Reason)) > 500 || (c.Decision != "publish" && c.Decision != "changes" && c.Decision != "archive" && c.Decision != "availability") {
			return Item{}, ErrInvalid
		}
	default:
		return Item{}, ErrInvalid
	}
	if c.ID == "" && c.Version != 0 || c.ID != "" && (!validID(c.ID) || c.Version < 1) {
		return Item{}, ErrInvalid
	}
	// One permanent actor row serializes retries and rate limits across instances.
	if err := active(ctx, s.DB, user, admin); err != nil {
		return Item{}, err
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO metar_market_actor(user_id) VALUES(?)`, user); err != nil {
		return Item{}, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		item, err := s.execute(ctx, user, key, c, admin)
		var dbErr *mysql.MySQLError
		if !errors.As(err, &dbErr) || dbErr.Number != 1213 {
			return item, err
		}
	}
	return Item{}, ErrConflict
}
func (s *Store) execute(ctx context.Context, user, key string, c Command, admin bool) (Item, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()
	var minute, day int64
	var commands, creates int
	if err = tx.QueryRowContext(ctx, `SELECT minute_bucket,commands,day_bucket,creates FROM metar_market_actor WHERE user_id=? FOR UPDATE`, user).Scan(&minute, &commands, &day, &creates); err != nil {
		return Item{}, err
	}
	if err = active(ctx, tx, user, admin); err != nil {
		return Item{}, err
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	fp := hex.EncodeToString(sum[:])
	var savedFP, response string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,response FROM metar_market_request WHERE actor_id=? AND request_key=?`, user, key).Scan(&savedFP, &response)
	if err == nil {
		if fp != savedFP {
			return Item{}, ErrConflict
		}
		if response != "" {
			var item Item
			if err = json.Unmarshal([]byte(response), &item); err != nil {
				return Item{}, err
			}
			return item, tx.Commit()
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Item{}, err
	}
	now := s.Now().Unix()
	if minute != now/60 {
		commands = 0
	}
	if day != (now+8*3600)/86400 {
		creates = 0
	}
	if commands >= 30 || (c.ID == "" && creates >= 20) {
		return Item{}, ErrLimit
	}
	if c.ID == "" {
		creates++
	}
	if _, err = tx.ExecContext(ctx, `UPDATE metar_market_actor SET minute_bucket=?,commands=?,day_bucket=?,creates=? WHERE user_id=?`, now/60, commands+1, (now+8*3600)/86400, creates, user); err != nil {
		return Item{}, err
	}
	var before stored
	id := c.ID
	if id == "" {
		if err = validatePost(ctx, tx, c.Entry, user, c.Op == "submit"); err != nil {
			return Item{}, err
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT id FROM metar_market_item WHERE question_id=?`, c.Entry.QuestionID).Scan(&existing)
		if err == nil {
			return Item{}, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Item{}, err
		}
		status := "draft"
		if c.Op == "submit" {
			status = "pending_review"
		}
		entry, _ := json.Marshal(c.Entry)
		result, e := tx.ExecContext(ctx, `INSERT INTO metar_market_item(question_id,author_id,status,entry_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`, c.Entry.QuestionID, user, status, string(entry), now, now)
		if e != nil {
			var dbErr *mysql.MySQLError
			if errors.As(e, &dbErr) && dbErr.Number == 1062 {
				return Item{}, ErrConflict
			}
			return Item{}, e
		}
		n, e := result.LastInsertId()
		if e != nil {
			return Item{}, e
		}
		id = strconv.FormatInt(n, 10)
	} else {
		before, err = loadStored(ctx, tx, id, true)
		if err != nil {
			return Item{}, err
		}
		if before.Version != c.Version {
			return Item{}, ErrConflict
		}
		if !admin && before.AuthorID != user {
			return Item{}, ErrForbidden
		}
		switch c.Op {
		case "save_draft", "submit":
			// Pending submissions are immutable. Published metadata only changes
			// through a new review; live_json and its run permissions stay intact.
			if before.Status == "pending_review" || (before.Status == "published" && c.Op != "submit") {
				return Item{}, ErrConflict
			}
			if before.QuestionID != c.Entry.QuestionID || before.Entry.Kind != c.Entry.Kind {
				return Item{}, ErrInvalid
			}
			if err = validatePost(ctx, tx, c.Entry, user, c.Op == "submit"); err != nil {
				return Item{}, err
			}
			entry, _ := json.Marshal(c.Entry)
			status := "draft"
			if c.Op == "submit" {
				status = "pending_review"
			}
			_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET entry_json=?,status=?,review_note='',version=version+1,updated_at=? WHERE id=?`, string(entry), status, now, id)
		case "archive":
			_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET status='archived',live_json=NULL,embed_allowed=0,featured=0,version=version+1,updated_at=? WHERE id=?`, now, id)
		case "review":
			switch c.Decision {
			case "publish":
				if before.Status != "pending_review" {
					return Item{}, ErrConflict
				}
				e, eErr := ValidateEntry(before.Entry, true)
				if eErr != nil {
					return Item{}, eErr
				}
				if err = validatePost(ctx, tx, e, before.AuthorID, true); err != nil {
					return Item{}, err
				}
				if c.EmbedAllowed && (!e.EmbedRequested || e.DemoURL == "") {
					return Item{}, ErrInvalid
				}
				if c.DemoStatus != "available" && c.DemoStatus != "unavailable" {
					return Item{}, ErrInvalid
				}
				_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET live_json=entry_json,status='published',review_note=?,embed_allowed=?,demo_status=?,featured=?,version=version+1,published_at=?,updated_at=? WHERE id=?`, c.Reason, c.EmbedAllowed, c.DemoStatus, c.Featured, now, now, id)
			case "changes":
				if before.Status != "pending_review" {
					return Item{}, ErrConflict
				}
				_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET status='changes_requested',review_note=?,version=version+1,updated_at=? WHERE id=?`, c.Reason, now, id)
			case "archive":
				_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET status='archived',live_json=NULL,embed_allowed=0,featured=0,review_note=?,version=version+1,updated_at=? WHERE id=?`, c.Reason, now, id)
			case "availability":
				if !before.Live.Valid || (c.DemoStatus != "available" && c.DemoStatus != "unavailable") {
					return Item{}, ErrConflict
				}
				var live Entry
				if json.Unmarshal([]byte(before.Live.String), &live) != nil {
					return Item{}, ErrInvalid
				}
				if c.EmbedAllowed && (!live.EmbedRequested || live.DemoURL == "") {
					return Item{}, ErrInvalid
				}
				_, err = tx.ExecContext(ctx, `UPDATE metar_market_item SET embed_allowed=?,demo_status=?,featured=?,review_note=?,version=version+1,updated_at=? WHERE id=?`, c.EmbedAllowed, c.DemoStatus, c.Featured, c.Reason, now, id)
			}
		}
		if err != nil {
			return Item{}, err
		}
	}
	after, err := loadStored(ctx, tx, id, false)
	if err != nil {
		return Item{}, err
	}
	item, err := s.privateItem(ctx, tx, after)
	if err != nil {
		return Item{}, err
	}
	old, _ := json.Marshal(before)
	next, _ := json.Marshal(after)
	if _, err = tx.ExecContext(ctx, `INSERT INTO metar_market_audit(actor_id,item_id,op,version,reason,before_json,after_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, user, id, c.Op, item.Version, c.Reason, string(old), string(next), now); err != nil {
		return Item{}, err
	}
	encoded, _ := json.Marshal(item)
	if _, err = tx.ExecContext(ctx, `INSERT INTO metar_market_request(actor_id,request_key,fingerprint,response,created_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE response=VALUES(response)`, user, key, fp, string(encoded), now); err != nil {
		return Item{}, err
	}
	return item, tx.Commit()
}

type stored struct {
	ID           string
	QuestionID   string
	AuthorID     string
	Status       string
	Entry        Entry
	Live         sql.NullString
	Version      int64
	ReviewNote   string
	EmbedAllowed bool
	DemoStatus   string
	Featured     bool
	CreatedAt    int64
	UpdatedAt    int64
	PublishedAt  int64
}

const storedColumns = `m.id,m.question_id,m.author_id,m.status,m.entry_json,m.live_json,m.version,m.review_note,m.embed_allowed,m.demo_status,m.featured,m.created_at,m.updated_at,m.published_at`

type scanner interface{ Scan(...any) error }

func scanStored(row scanner) (stored, error) {
	var v stored
	var raw string
	err := row.Scan(&v.ID, &v.QuestionID, &v.AuthorID, &v.Status, &raw, &v.Live, &v.Version, &v.ReviewNote, &v.EmbedAllowed, &v.DemoStatus, &v.Featured, &v.CreatedAt, &v.UpdatedAt, &v.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v.Entry)
	return v, err
}
func loadStored(ctx context.Context, q querier, id string, lock bool) (stored, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	return scanStored(q.QueryRowContext(ctx, `SELECT `+storedColumns+` FROM metar_market_item m WHERE m.id=?`+suffix, id))
}
func (s *Store) privateItem(ctx context.Context, q querier, v stored) (Item, error) {
	out := Item{ID: v.ID, Entry: v.Entry, Status: v.Status, Version: v.Version, ReviewNote: v.ReviewNote, EmbedAllowed: v.EmbedAllowed, DemoStatus: v.DemoStatus, Featured: v.Featured, HasPublished: v.Live.Valid, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, PublishedAt: v.PublishedAt}
	err := q.QueryRowContext(ctx, "SELECT q.title,COALESCE(u.username,''),COALESCE(u.display_name,''),COALESCE(u.avatar,''),q.answer_count,q.view_count FROM question q LEFT JOIN `user` u ON u.id=q.user_id WHERE q.id=?", v.QuestionID).Scan(&out.Title, &out.Author.Username, &out.Author.Name, &out.Author.Avatar, &out.AnswerCount, &out.ViewCount)
	return out, err
}
