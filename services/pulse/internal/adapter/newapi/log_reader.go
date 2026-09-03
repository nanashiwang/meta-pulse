// Package newapi contains read-only adapters for new-api data sources.
package newapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"github.com/nanashiwang/meta-pulse/internal/domain/usage"
)

const (
	LogTypeConsume = 2
	LogTypeRefund  = 6
)

type LogRecord struct {
	ID        int64
	UserID    int64
	CreatedAt int64
	Type      int
	ModelName string
	Quota     int64
	ChannelID int64
	RequestID string
	Other     string
}

type Cursor struct {
	CreatedAt int64
	ID        int64
}

func (c Cursor) String() string {
	return strconv.FormatInt(c.CreatedAt, 10) + ":" + strconv.FormatInt(c.ID, 10)
}

func ParseCursor(value string) (Cursor, error) {
	if strings.TrimSpace(value) == "" {
		return Cursor{}, nil
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return Cursor{}, fmt.Errorf("invalid log cursor %q", value)
	}
	createdAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || createdAt < 0 {
		return Cursor{}, fmt.Errorf("invalid log cursor timestamp %q", value)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id < 0 {
		return Cursor{}, fmt.Errorf("invalid log cursor id %q", value)
	}
	return Cursor{CreatedAt: createdAt, ID: id}, nil
}

type LogReader struct {
	db *sql.DB
}

func OpenLogReader(dsn string) (*LogReader, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("new-api log DSN is empty")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open new-api log database: %w", err)
	}
	// This adapter exposes SELECT only. The account must additionally be
	// provisioned with SELECT privileges by deployment; code never receives a
	// write-capable handle to Pulse's or new-api's primary database.
	db.SetMaxOpenConns(5)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping new-api log database: %w", err)
	}
	return &LogReader{db: db}, nil
}

func NewLogReaderForDB(db *sql.DB) (*LogReader, error) {
	if db == nil {
		return nil, errors.New("new-api log database is nil")
	}
	return &LogReader{db: db}, nil
}

func (r *LogReader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *LogReader) Fetch(ctx context.Context, after Cursor, limit int) ([]LogRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("new-api log reader is not initialized")
	}
	if limit <= 0 || limit > 5000 {
		return nil, errors.New("log batch size must be between 1 and 5000")
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, created_at, type, model_name, quota, channel, request_id, other
FROM logs
WHERE type IN (?, ?)
  AND (created_at > ? OR (created_at = ? AND id > ?))
ORDER BY created_at ASC, id ASC
LIMIT ?`, LogTypeConsume, LogTypeRefund, after.CreatedAt, after.CreatedAt, after.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("read new-api logs: %w", err)
	}
	defer rows.Close()

	result := make([]LogRecord, 0, limit)
	for rows.Next() {
		var record LogRecord
		var requestID, other sql.NullString
		if err := rows.Scan(&record.ID, &record.UserID, &record.CreatedAt, &record.Type, &record.ModelName, &record.Quota, &record.ChannelID, &requestID, &other); err != nil {
			return nil, fmt.Errorf("scan new-api log: %w", err)
		}
		record.RequestID = requestID.String
		record.Other = other.String
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate new-api logs: %w", err)
	}
	return result, nil
}

type logPayload struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
	Type      int    `json:"type"`
	ModelName string `json:"model_name"`
	Quota     int64  `json:"quota"`
	ChannelID int64  `json:"channel_id"`
	RequestID string `json:"request_id"`
	Other     string `json:"other"`
}

func (r LogRecord) PayloadHash() (string, error) {
	payload, err := json.Marshal(logPayload{ID: r.ID, UserID: r.UserID, CreatedAt: r.CreatedAt, Type: r.Type, ModelName: r.ModelName, Quota: r.Quota, ChannelID: r.ChannelID, RequestID: r.RequestID, Other: r.Other})
	if err != nil {
		return "", fmt.Errorf("marshal log fingerprint: %w", err)
	}
	return sha256Hex(payload), nil
}

// LogSource composes the read-only cursor with the normalized mapper. Both
// realtime ingest and backfill use this exact source contract.
type LogSource struct {
	reader *LogReader
	mapper UsageMapper
}

func NewLogSource(reader *LogReader, sourceSystem string) (*LogSource, error) {
	if reader == nil {
		return nil, errors.New("new-api log reader is nil")
	}
	if sourceSystem == "" {
		sourceSystem = "new-api-log"
	}
	return &LogSource{reader: reader, mapper: UsageMapper{SourceSystem: sourceSystem}}, nil
}

func (s *LogSource) Fetch(ctx context.Context, after string, limit int) ([]usage.Event, error) {
	cursor, err := ParseCursor(after)
	if err != nil {
		return nil, err
	}
	records, err := s.reader.Fetch(ctx, cursor, limit)
	if err != nil {
		return nil, err
	}
	events := make([]usage.Event, 0, len(records))
	for _, record := range records {
		event, err := s.mapper.Map(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}
