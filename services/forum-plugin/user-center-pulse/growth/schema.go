package growth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Plugin-owned tables live in the Answer database and are included in its
// normal backup. All timestamps are UTC unix seconds, all experience integers.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS metar_exp_config (id INT PRIMARY KEY, version BIGINT NOT NULL, rules_json TEXT NOT NULL, started_at BIGINT NOT NULL) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_account (user_id BIGINT UNSIGNED PRIMARY KEY, balance BIGINT NOT NULL DEFAULT 0, last_checkin CHAR(10) NOT NULL DEFAULT '', streak INT NOT NULL DEFAULT 0, appearance VARCHAR(24) NOT NULL DEFAULT 'default') ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_event (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, source_key VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE, user_id BIGINT UNSIGNED NOT NULL, kind VARCHAR(24) NOT NULL, object_type VARCHAR(16) NOT NULL DEFAULT '', object_id BIGINT UNSIGNED NOT NULL DEFAULT 0, actor_id BIGINT UNSIGNED NOT NULL DEFAULT 0, occurred_at BIGINT NOT NULL, eligible_at BIGINT NOT NULL, earned_day CHAR(10) NOT NULL, amount BIGINT NOT NULL, daily_cap BIGINT NOT NULL, daily_count INT NOT NULL, daily_amount BIGINT NOT NULL, rule_version BIGINT NOT NULL, status VARCHAR(16) NOT NULL, fingerprint CHAR(64) NOT NULL, reason VARCHAR(500) NOT NULL DEFAULT '', INDEX pending(status,eligible_at,id), INDEX owner(user_id,id), INDEX quota(user_id,earned_day,kind,status)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_ledger (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, entry_key VARCHAR(191) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE, user_id BIGINT UNSIGNED NOT NULL, event_id BIGINT UNSIGNED NOT NULL, delta BIGINT NOT NULL, kind VARCHAR(24) NOT NULL, balance BIGINT NOT NULL, reason VARCHAR(500) NOT NULL, actor_id BIGINT UNSIGNED NOT NULL DEFAULT 0, created_at BIGINT NOT NULL, INDEX owner(user_id,id)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_cursor (name VARCHAR(24) PRIMARY KEY, last_id BIGINT UNSIGNED NOT NULL DEFAULT 0) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_notice (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, user_id BIGINT UNSIGNED NOT NULL, level INT NOT NULL, created_at BIGINT NOT NULL, read_at BIGINT NOT NULL DEFAULT 0, UNIQUE KEY once_level(user_id,level)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_exp_audit (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, request_key VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE, fingerprint CHAR(64) NOT NULL, actor_id BIGINT UNSIGNED NOT NULL, operation VARCHAR(24) NOT NULL, reason VARCHAR(500) NOT NULL, details TEXT NOT NULL, created_at BIGINT NOT NULL) ENGINE=InnoDB`,
}

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{DB: db, Now: time.Now} }
func (s *Store) Ensure(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('metar_exp_schema_v1',10)`).Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return errors.New("experience migration busy")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK('metar_exp_schema_v1')`)
	}()
	for _, ddl := range schema {
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}

	for _, table := range []string{"metar_exp_ledger", "metar_exp_audit"} {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			name := table + "_no_" + strings.ToLower(operation)
			var action string
			err := conn.QueryRowContext(ctx, "SELECT ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?", name).Scan(&action)
			expected := "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'community experience history is append-only'"
			if errors.Is(err, sql.ErrNoRows) {
				_, err = conn.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER %s BEFORE %s ON %s FOR EACH ROW %s", name, operation, table, expected))
			} else if err == nil && strings.Join(strings.Fields(action), " ") != expected {
				return errors.New("experience history guard has unexpected definition")
			}
			if err != nil {
				return err
			}
		}
	}
	data, _ := json.Marshal(DefaultRules())
	_, err = conn.ExecContext(ctx, `INSERT IGNORE INTO metar_exp_config(id,version,rules_json,started_at) VALUES(1,1,?,?)`, string(data), s.Now().Unix())
	return err
}

type Settings struct {
	Version   int64   `json:"version"`
	Rules     Rules   `json:"rules"`
	StartedAt int64   `json:"started_at"`
	Levels    []Level `json:"levels"`
}
type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func settings(ctx context.Context, q queryer) (Settings, error) {
	var out Settings
	var raw string
	err := q.QueryRowContext(ctx, `SELECT version,rules_json,started_at FROM metar_exp_config WHERE id=1`).Scan(&out.Version, &raw, &out.StartedAt)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal([]byte(raw), &out.Rules)
	if err == nil {
		err = out.Rules.Validate()
	}
	out.Levels = Levels
	return out, err
}
func (s *Store) Settings(ctx context.Context) (Settings, error) { return settings(ctx, s.DB) }
