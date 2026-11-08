-- +goose Up
-- Admin imports record who ran them and the client idempotency key, so a
-- retried import returns the original result instead of writing again.
ALTER TABLE intel_import_run
  ADD COLUMN actor_id VARCHAR(32) NULL AFTER note,
  ADD COLUMN idempotency_key CHAR(36) NULL AFTER actor_id,
  ADD COLUMN request_fingerprint CHAR(64) NULL AFTER idempotency_key,
  ADD UNIQUE KEY uk_intel_import_run_idempotency (idempotency_key);

-- Signed admin requests claim their nonce here; rows past expiry are pruned.
CREATE TABLE intel_admin_nonce (
  nonce VARCHAR(128) NOT NULL PRIMARY KEY,
  expires_at DATETIME(3) NOT NULL,
  KEY idx_intel_admin_nonce_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down
DROP TABLE intel_admin_nonce;
ALTER TABLE intel_import_run
  DROP INDEX uk_intel_import_run_idempotency,
  DROP COLUMN request_fingerprint,
  DROP COLUMN idempotency_key,
  DROP COLUMN actor_id;
