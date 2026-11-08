package mysql

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SourceState is one row of intel_source_state. KeyEnc is ciphertext; the
// plaintext key never touches this package.
type SourceState struct {
	Source          string
	Enabled         bool
	KeyEnc          []byte
	KeyHint         string
	KeyID           string
	UpdatedBy       string
	UpdatedAt       time.Time
	SyncRequestedAt *time.Time
	LastAttemptAt   *time.Time
	LastSuccessAt   *time.Time
	LastStatus      string
	LastError       string
	LastReport      string
	LastRequests    int
}

func (s *Store) SourceState(ctx context.Context, source string) (SourceState, bool, error) {
	st := SourceState{Source: source, Enabled: true}
	var req, attempt, success sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT enabled, api_key_enc, key_hint, key_id, updated_by, updated_at, sync_requested_at, last_attempt_at,
			last_success_at, last_status, last_error, last_report, last_requests FROM intel_source_state WHERE source_slug = ?`, source).
		Scan(&st.Enabled, &st.KeyEnc, &st.KeyHint, &st.KeyID, &st.UpdatedBy, &st.UpdatedAt, &req, &attempt, &success, &st.LastStatus, &st.LastError, &st.LastReport, &st.LastRequests)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	st.SyncRequestedAt, st.LastAttemptAt, st.LastSuccessAt = nullTime(req), nullTime(attempt), nullTime(success)
	st.UpdatedAt = st.UpdatedAt.UTC()
	return st, true, nil
}

// SaveSourceKey stores a new encrypted key and enables the source.
func (s *Store) SaveSourceKey(ctx context.Context, source string, enc []byte, hint, keyID, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO intel_source_state (source_slug, enabled, api_key_enc, key_hint, key_id, updated_by) VALUES (?, 1, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE enabled = 1, api_key_enc = VALUES(api_key_enc), key_hint = VALUES(key_hint), key_id = VALUES(key_id), updated_by = VALUES(updated_by)`,
		source, enc, hint, keyID, actor)
	return err
}

func (s *Store) ClearSourceKey(ctx context.Context, source, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO intel_source_state (source_slug, enabled, updated_by) VALUES (?, 0, ?)
		ON DUPLICATE KEY UPDATE api_key_enc = NULL, key_hint = '', key_id = '', enabled = 0, sync_requested_at = NULL, updated_by = VALUES(updated_by)`, source, actor)
	return err
}

func (s *Store) SetSourceEnabled(ctx context.Context, source string, enabled bool, actor string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO intel_source_state (source_slug, enabled, updated_by) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE enabled = VALUES(enabled), updated_by = VALUES(updated_by)`, source, enabled, actor)
	return err
}

// RequestSourceSync asks the worker to sync on its next short poll.
func (s *Store) RequestSourceSync(ctx context.Context, source, actor string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO intel_source_state (source_slug, enabled, sync_requested_at, updated_by) VALUES (?, 1, ?, ?)
		ON DUPLICATE KEY UPDATE sync_requested_at = VALUES(sync_requested_at)`, source, now.UTC(), actor)
	return err
}

// RecordSourceSync stores the outcome of an attempt. A request made after
// the attempt started is kept, so it is not silently dropped.
func (s *Store) RecordSourceSync(ctx context.Context, source string, started time.Time, ok bool, status, errMsg, report string, requests int) error {
	if len(errMsg) > 300 {
		errMsg = errMsg[:300]
	}
	if len(report) > 1000 {
		report = report[:1000]
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO intel_source_state (source_slug, enabled, last_attempt_at, last_success_at, last_status, last_error, last_report, last_requests)
		VALUES (?, 1, ?, IF(?, ?, NULL), ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE last_attempt_at = VALUES(last_attempt_at),
		  last_success_at = IF(?, VALUES(last_attempt_at), last_success_at),
		  last_status = VALUES(last_status), last_error = VALUES(last_error), last_report = VALUES(last_report), last_requests = VALUES(last_requests),
		  sync_requested_at = IF(sync_requested_at IS NOT NULL AND sync_requested_at <= VALUES(last_attempt_at), NULL, sync_requested_at)`,
		source, started.UTC(), ok, started.UTC(), status, errMsg, report, requests, ok)
	return err
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}
