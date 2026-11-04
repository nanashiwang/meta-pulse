package analytics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"time"
)

var ErrInvalid = errors.New("invalid_request")
var ErrForbidden = errors.New("forbidden")
var ErrConflict = errors.New("conflict")
var ErrLimit = errors.New("analytics_limit")
var CookiePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var sourcePattern = regexp.MustCompile(`^[a-zA-Z0-9.-]*$`)
var KeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,96}$`)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{db, time.Now} }
func NewVisitor() (string, error) {
	v := make([]byte, 32)
	_, e := rand.Read(v)
	return hex.EncodeToString(v), e
}
func VisitorHash(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func validID(id string) bool {
	n, e := strconv.ParseUint(id, 10, 64)
	return e == nil && n > 0 && strconv.FormatUint(n, 10) == id
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func checkAccount(ctx context.Context, q queryer, id string, admin bool) error {
	if !validID(id) {
		return ErrForbidden
	}
	var status, mail, roles int
	e := q.QueryRowContext(ctx, "SELECT u.status,u.mail_status,(SELECT COUNT(*) FROM user_role_rel r WHERE r.user_id=u.id AND r.role_id=2) FROM `user` u WHERE u.id=?", id).Scan(&status, &mail, &roles)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrForbidden
	}
	if e != nil {
		return e
	}
	if status != 1 || mail != 1 || (admin && roles != 1) {
		return ErrForbidden
	}
	return nil
}
func (s *Store) CheckAdmin(ctx context.Context, id string) error {
	return checkAccount(ctx, s.DB, id, true)
}
func (s *Store) Ensure(ctx context.Context) error {
	c, e := s.DB.Conn(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	var locked int
	if e = c.QueryRowContext(ctx, "SELECT GET_LOCK('metar_ops_schema_v1',2)").Scan(&locked); e != nil {
		return e
	}
	if locked != 1 {
		return errors.New("analytics schema busy")
	}
	defer func() {
		x, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		c.ExecContext(x, "SELECT RELEASE_LOCK('metar_ops_schema_v1')")
	}()
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS metar_ops_config(id INT PRIMARY KEY,started_at BIGINT NOT NULL) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_ops_visitor(id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,first_seen BIGINT NOT NULL,last_seen BIGINT NOT NULL,source VARCHAR(128) NOT NULL,existing_user BOOLEAN NOT NULL DEFAULT FALSE,converted_user BIGINT UNSIGNED NULL,registered_at BIGINT NULL,UNIQUE KEY converted_user(converted_user),INDEX first_seen(first_seen)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_ops_day(day CHAR(10) CHARACTER SET ascii NOT NULL,subject VARCHAR(66) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,user_id BIGINT UNSIGNED NULL,effective BOOLEAN NOT NULL DEFAULT FALSE,first_seen BIGINT NOT NULL,last_seen BIGINT NOT NULL,PRIMARY KEY(day,subject),INDEX user_day(user_id,day)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_ops_member(user_id BIGINT UNSIGNED PRIMARY KEY,version BIGINT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS metar_ops_label(user_id BIGINT UNSIGNED NOT NULL,label VARCHAR(32) COLLATE utf8mb4_bin NOT NULL,PRIMARY KEY(user_id,label),INDEX label(label,user_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS metar_ops_audit(actor_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,fingerprint CHAR(64) NOT NULL,user_id BIGINT UNSIGNED NOT NULL,version BIGINT NOT NULL,previous_labels TEXT NOT NULL,labels TEXT NOT NULL,created_at BIGINT NOT NULL,PRIMARY KEY(actor_id,request_key),INDEX created_at(created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, e = c.ExecContext(ctx, ddl); e != nil {
			return e
		}
	}
	_, e = c.ExecContext(ctx, "INSERT IGNORE INTO metar_ops_config VALUES(1,?)", s.Now().Unix())
	return e
}

type Visit struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

func (v Visit) Validate() error {
	if v.Kind != "visit" && v.Kind != "read" {
		return ErrInvalid
	}
	if len(v.Source) > 128 || !sourcePattern.MatchString(v.Source) {
		return ErrInvalid
	}
	return nil
}

// Only the authenticated route may supply a user. Cookie identity never grants access.
func (s *Store) Track(ctx context.Context, cookie, user string, v Visit) error {
	if !CookiePattern.MatchString(cookie) || v.Validate() != nil {
		return ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if user != "" {
		if e = checkAccount(ctx, tx, user, false); e != nil {
			return e
		}
	}
	now := s.Now()
	stamp := now.Unix()
	hash := VisitorHash(cookie)
	source := v.Source
	if source == "" {
		source = "direct"
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO metar_ops_visitor(id,first_seen,last_seen,source) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE last_seen=GREATEST(last_seen,VALUES(last_seen))`, hash, stamp, stamp, source)
	if e != nil {
		return e
	}
	if user != "" {
		// Known existing accounts are not prospects, even if they browsed anonymously.
		_, e = tx.ExecContext(ctx, "UPDATE metar_ops_visitor v JOIN `user` u ON u.id=? SET v.existing_user=TRUE WHERE v.id=? AND v.converted_user IS NULL AND UNIX_TIMESTAMP(u.created_at)<v.first_seen", user, hash)
		if e != nil {
			return e
		}
		// Associate only a registration observed after this browser's first visit. An
		// existing account logging in is not a new visitor-to-registration conversion.
		_, e = tx.ExecContext(ctx, `UPDATE IGNORE metar_ops_visitor v JOIN `+"`user`"+` u ON u.id=? SET v.converted_user=u.id,v.registered_at=UNIX_TIMESTAMP(u.created_at) WHERE v.id=? AND v.converted_user IS NULL AND UNIX_TIMESTAMP(u.created_at)>=v.first_seen AND UNIX_TIMESTAMP(u.created_at)<=? AND UNIX_TIMESTAMP(u.created_at)-v.first_seen<=2592000`, user, hash, stamp)
		if e != nil {
			return e
		}
	}
	subject := "v:" + hash
	var uid any
	if user != "" {
		subject = "u:" + user
		uid = user
	}
	// Read events need an earlier visit on this day; retries only upgrade a boolean.
	_, e = tx.ExecContext(ctx, `INSERT INTO metar_ops_day(day,subject,user_id,first_seen,last_seen) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE effective=effective OR (? AND ?>=first_seen+30),last_seen=GREATEST(last_seen,VALUES(last_seen))`, Day(now), subject, uid, stamp, stamp, v.Kind == "read", stamp)
	if e != nil {
		return e
	}
	return tx.Commit()
}
