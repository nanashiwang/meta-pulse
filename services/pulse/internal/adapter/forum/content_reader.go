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

// Fetch reads only public question metadata authored after an immutable
// Meta API binding exists. Answer's local user_id is never a new-api identity;
// the guarded user_external_login projection is the only allowed mapping.
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
	// Page by the raw Answer question id before applying eligibility. Returning
	// cursor-only events for excluded rows prevents a tail of unbound/hidden
	// content from being rescanned on every worker tick.
	rows, err := r.db.QueryContext(ctx, `
SELECT q.id, binding.meta_pulse_external_id_guard, q.title,
       UNIX_TIMESTAMP(q.created_at), q.show, q.status
FROM (
  SELECT id, user_id, title, created_at, `+"`show`"+`, status
  FROM question
  WHERE id > ?
  ORDER BY id ASC
  LIMIT ?
) AS q
LEFT JOIN user_external_login AS binding
  ON binding.meta_pulse_user_id_guard = q.user_id
 AND binding.created_at < q.created_at
ORDER BY q.id ASC`, lastID, limit)
	if err != nil {
		return nil, fmt.Errorf("read forum questions: %w", err)
	}
	defer rows.Close()
	result := make([]ports.ContentEvent, 0, limit)
	previousID := lastID
	for rows.Next() {
		var id, createdAt int64
		var externalID, title sql.NullString
		var show, status int
		if err := rows.Scan(&id, &externalID, &title, &createdAt, &show, &status); err != nil {
			return nil, fmt.Errorf("scan forum question: %w", err)
		}
		if id <= previousID || createdAt <= 0 {
			return nil, fmt.Errorf("forum returned a non-monotonic or invalid question cursor row %d", id)
		}
		previousID = id
		event := ports.ContentEvent{
			SkipCandidate:   show != 1 || (status != 1 && status != 2) || !externalID.Valid,
			SourceContentID: strconv.FormatInt(id, 10),
			ContentType:     "question",
			Title:           title.String,
			SourceCreatedAt: time.Unix(createdAt, 0).UTC(),
			CursorValue:     strconv.FormatInt(id, 10),
		}
		if event.SkipCandidate {
			result = append(result, event)
			continue
		}
		userID, err := strconv.ParseUint(externalID.String, 10, 64)
		if err != nil || userID == 0 || strconv.FormatUint(userID, 10) != externalID.String {
			return nil, fmt.Errorf("forum binding guard returned an invalid identity for question %d", id)
		}
		event.AuthorUserID = userID
		payload, _ := json.Marshal(struct {
			ID        int64  `json:"id"`
			UserID    uint64 `json:"user_id"`
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
