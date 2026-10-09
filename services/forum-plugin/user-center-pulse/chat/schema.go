// Package chat owns private community messages in the Answer database.
package chat

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Store struct {
	DB  *sql.DB
	Now func() time.Time
}

func New(db *sql.DB) *Store { return &Store{DB: db, Now: time.Now} }

var schema = []string{
	`CREATE TABLE IF NOT EXISTS metar_chat_room (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, kind VARCHAR(8) NOT NULL, pair_key VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin NULL UNIQUE, title VARCHAR(80) NOT NULL DEFAULT '', owner_id BIGINT UNSIGNED NOT NULL, seq BIGINT NOT NULL DEFAULT 0, version BIGINT NOT NULL DEFAULT 0, closed BOOLEAN NOT NULL DEFAULT 0, updated_at BIGINT NOT NULL, INDEX updated(updated_at,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS metar_chat_member (room_id BIGINT UNSIGNED NOT NULL, user_id BIGINT UNSIGNED NOT NULL, state VARCHAR(12) NOT NULL, since_seq BIGINT NOT NULL DEFAULT 1, read_seq BIGINT NOT NULL DEFAULT 0, unread_count BIGINT NOT NULL DEFAULT 0, invited_by BIGINT UNSIGNED NOT NULL DEFAULT 0, PRIMARY KEY(room_id,user_id), INDEX inbox(user_id,state,room_id)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_chat_message (room_id BIGINT UNSIGNED NOT NULL, seq BIGINT NOT NULL, sender_id BIGINT UNSIGNED NOT NULL, body TEXT NOT NULL, created_at BIGINT NOT NULL, removed BOOLEAN NOT NULL DEFAULT 0, version BIGINT NOT NULL, INDEX changes(room_id,version), PRIMARY KEY(room_id,seq)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS metar_chat_block (user_id BIGINT UNSIGNED NOT NULL, target_id BIGINT UNSIGNED NOT NULL, PRIMARY KEY(user_id,target_id)) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_chat_request (user_id BIGINT UNSIGNED NOT NULL, request_key VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL, fingerprint CHAR(64) NOT NULL, response TEXT NOT NULL, created_at BIGINT NOT NULL, PRIMARY KEY(user_id,request_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE IF NOT EXISTS metar_chat_limit (user_id BIGINT UNSIGNED PRIMARY KEY, minute_bucket BIGINT NOT NULL DEFAULT 0, minute_count INT NOT NULL DEFAULT 0, day_bucket BIGINT NOT NULL DEFAULT 0, message_count INT NOT NULL DEFAULT 0, create_count INT NOT NULL DEFAULT 0, invite_count INT NOT NULL DEFAULT 0, report_count INT NOT NULL DEFAULT 0) ENGINE=InnoDB`,
	`CREATE TABLE IF NOT EXISTS metar_chat_report (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, room_id BIGINT UNSIGNED NOT NULL, seq BIGINT NOT NULL, reporter_id BIGINT UNSIGNED NOT NULL, sender_id BIGINT UNSIGNED NOT NULL, body TEXT NOT NULL, reason VARCHAR(500) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'pending', resolution VARCHAR(500) NOT NULL DEFAULT '', reviewer_id BIGINT UNSIGNED NOT NULL DEFAULT 0, created_at BIGINT NOT NULL, reviewed_at BIGINT NOT NULL DEFAULT 0, UNIQUE KEY once_report(room_id,seq,reporter_id), INDEX queue(status,id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
}

func (s *Store) Ensure(ctx context.Context) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var lock int
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('metar_chat_schema_v1',10)`).Scan(&lock); err != nil {
		return err
	}
	if lock != 1 {
		return errors.New("chat migration busy")
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, `SELECT RELEASE_LOCK('metar_chat_schema_v1')`)
	}()
	for _, q := range schema {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
