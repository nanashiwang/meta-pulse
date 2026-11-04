package analytics

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type LabelCommand struct {
	UserID  string   `json:"user_id"`
	Version int64    `json:"version"`
	Labels  []string `json:"labels"`
}

func (c *LabelCommand) Validate() error {
	if !validID(c.UserID) || c.Version < 0 || len(c.Labels) > 12 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, v := range c.Labels {
		if strings.ContainsAny(v, ",，") || !utf8.ValidString(v) || strings.TrimSpace(v) != v || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > 32 || seen[v] {
			return ErrInvalid
		}
		for _, r := range v {
			if unicode.IsControl(r) {
				return ErrInvalid
			}
		}
		seen[v] = true
	}
	sort.Strings(c.Labels)
	return nil
}
func readLabels(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, user string) ([]string, error) {
	out := []string{}
	rows, e := q.QueryContext(ctx, "SELECT label FROM metar_ops_label WHERE user_id=? ORDER BY label", user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveLabels(ctx context.Context, admin, key string, c LabelCommand) (int64, error) {
	if !KeyPattern.MatchString(key) || c.Validate() != nil {
		return 0, ErrInvalid
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(sum[:])
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if e = checkAccount(ctx, tx, admin, true); e != nil {
		return 0, e
	}
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user` WHERE id=?", c.UserID).Scan(&exists); e != nil {
		return 0, e
	}
	if exists != 1 {
		return 0, ErrInvalid
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO metar_ops_member(user_id) VALUES(?) ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)", c.UserID)
	if e != nil {
		return 0, e
	}
	var version int64
	if e = tx.QueryRowContext(ctx, "SELECT version FROM metar_ops_member WHERE user_id=? FOR UPDATE", c.UserID).Scan(&version); e != nil {
		return 0, e
	}
	var oldfingerprint string
	var oldversion int64
	e = tx.QueryRowContext(ctx, "SELECT fingerprint,version FROM metar_ops_audit WHERE actor_id=? AND request_key=? FOR UPDATE", admin, key).Scan(&oldfingerprint, &oldversion)
	if e == nil {
		if oldfingerprint != fingerprint {
			return 0, ErrConflict
		}
		return oldversion, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	if version != c.Version {
		return 0, ErrConflict
	}
	previous, e := readLabels(ctx, tx, c.UserID)
	if e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM metar_ops_label WHERE user_id=?", c.UserID); e != nil {
		return 0, e
	}
	for _, v := range c.Labels {
		if _, e = tx.ExecContext(ctx, "INSERT INTO metar_ops_label(user_id,label) VALUES(?,?)", c.UserID, v); e != nil {
			return 0, e
		}
	}
	version++
	if _, e = tx.ExecContext(ctx, "UPDATE metar_ops_member SET version=? WHERE user_id=?", version, c.UserID); e != nil {
		return 0, e
	}
	prev, _ := json.Marshal(previous)
	next, _ := json.Marshal(c.Labels)
	_, e = tx.ExecContext(ctx, "INSERT INTO metar_ops_audit(actor_id,request_key,fingerprint,user_id,version,previous_labels,labels,created_at) VALUES(?,?,?,?,?,?,?,?)", admin, key, fingerprint, c.UserID, version, string(prev), string(next), s.Now().Unix())
	if e != nil {
		var duplicate *mysql.MySQLError
		if errors.As(e, &duplicate) && duplicate.Number == 1062 {
			return 0, ErrConflict
		}
		return 0, e
	}
	return version, tx.Commit()
}

type Member struct {
	ID         string   `json:"id"`
	Username   string   `json:"username"`
	Name       string   `json:"display_name"`
	Registered int64    `json:"registered_at"`
	Version    int64    `json:"version"`
	Labels     []string `json:"labels"`
}
type Members struct {
	Items []Member `json:"items"`
	More  bool     `json:"more"`
}

func (s *Store) Members(ctx context.Context, admin, search, label string, page int) (Members, error) {
	out := Members{Items: []Member{}}
	if e := s.CheckAdmin(ctx, admin); e != nil {
		return out, e
	}
	if page < 1 || page > 1000 || utf8.RuneCountInString(search) > 50 || utf8.RuneCountInString(label) > 32 {
		return out, ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, "SELECT u.id,u.username,u.display_name,UNIX_TIMESTAMP(u.created_at),COALESCE(m.version,0) FROM `user` u LEFT JOIN metar_ops_member m ON m.user_id=u.id WHERE (?='' OR LOCATE(?,u.username)>0 OR LOCATE(?,u.display_name)>0) AND (?='' OR EXISTS(SELECT 1 FROM metar_ops_label l WHERE l.user_id=u.id AND l.label=?)) ORDER BY u.id DESC LIMIT 26 OFFSET ?", search, search, search, label, label, (page-1)*25)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v Member
		if e = rows.Scan(&v.ID, &v.Username, &v.Name, &v.Registered, &v.Version); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Items) > 25 {
		out.More = true
		out.Items = out.Items[:25]
	}
	for i := range out.Items {
		out.Items[i].Labels, e = readLabels(ctx, s.DB, out.Items[i].ID)
		if e != nil {
			return out, e
		}
	}
	return out, nil
}
