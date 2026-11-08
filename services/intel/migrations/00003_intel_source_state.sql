-- +goose Up
-- Per-source sync settings and status. API keys are stored only encrypted
-- (AES-256-GCM, key derived from INTEL_ADMIN_HMAC_SECRET); key_hint keeps
-- the last 4 characters for display and key_id detects a rotated secret.
CREATE TABLE intel_source_state (
  source_slug VARCHAR(80) NOT NULL PRIMARY KEY,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  api_key_enc VARBINARY(1024) NULL,
  key_hint VARCHAR(8) NOT NULL DEFAULT '',
  key_id CHAR(16) NOT NULL DEFAULT '',
  updated_by VARCHAR(32) NOT NULL DEFAULT '',
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  sync_requested_at DATETIME(3) NULL,
  last_attempt_at DATETIME(3) NULL,
  last_success_at DATETIME(3) NULL,
  last_status VARCHAR(16) NOT NULL DEFAULT '',
  last_error VARCHAR(300) NOT NULL DEFAULT '',
  last_report VARCHAR(1000) NOT NULL DEFAULT '',
  last_requests INT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down
DROP TABLE intel_source_state;
