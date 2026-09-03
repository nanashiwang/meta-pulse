// Package forum contains the read-only Answer database adapter.
package forum

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type Reader struct{ db *sql.DB }

func OpenReader(dsn string) (*Reader, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("forum database DSN is empty")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open forum database: %w", err)
	}
	db.SetMaxOpenConns(5)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping forum database: %w", err)
	}
	return &Reader{db: db}, nil
}

func NewReaderForDB(db *sql.DB) (*Reader, error) {
	if db == nil {
		return nil, errors.New("forum database is nil")
	}
	return &Reader{db: db}, nil
}

func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// Fetch reads only public question metadata. Answer's v1 schema uses
// answer_question and Unix-second create_time; deployment can replace this
// reader if its Answer schema is customized without changing the ingest port.
func (r *Reader) Fetch(ctx context.Context, after string, limit int) ([]ports.ContentEvent, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("forum reader is not initialized")
	}
	if limit <= 0 || limit > 5000 {
		return nil, errors.New("content batch size must be between 1 and 5000")
	}
	lastID := int64(0)
	if strings.TrimSpace(after) != "" {
		var err error
		lastID, err = strconv.ParseInt(after, 10, 64)
		if err != nil || lastID < 0 {
			return nil, fmt.Errorf("invalid forum content cursor %q", after)
		}
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, title, create_time
FROM answer_question
WHERE id > ?
ORDER BY id ASC
LIMIT ?`, lastID, limit)
	if err != nil {
		return nil, fmt.Errorf("read forum questions: %w", err)
	}
	defer rows.Close()
	result := make([]ports.ContentEvent, 0, limit)
	for rows.Next() {
		var id, userID, createdAt int64
		var title sql.NullString
		if err := rows.Scan(&id, &userID, &title, &createdAt); err != nil {
			return nil, fmt.Errorf("scan forum question: %w", err)
		}
		if id <= 0 || userID <= 0 || createdAt <= 0 {
			continue
		}
		event := ports.ContentEvent{SourceContentID: strconv.FormatInt(id, 10), ContentType: "question", AuthorUserID: uint64(userID), Title: title.String, SourceCreatedAt: time.Unix(createdAt, 0).UTC(), CursorValue: strconv.FormatInt(id, 10)}
		payload, _ := json.Marshal(struct {
			ID        int64  `json:"id"`
			UserID    int64  `json:"user_id"`
			Title     string `json:"title"`
			CreatedAt int64  `json:"created_at"`
		}{id, userID, title.String, createdAt})
		digest := sha256.Sum256(payload)
		event.PayloadHash = hex.EncodeToString(digest[:])
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate forum questions: %w", err)
	}
	return result, nil
}
