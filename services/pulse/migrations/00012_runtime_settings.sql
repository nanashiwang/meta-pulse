-- +goose Up
CREATE TABLE IF NOT EXISTS pulse_runtime_config (
    id TINYINT UNSIGNED NOT NULL,
    revision BIGINT UNSIGNED NOT NULL DEFAULT 0,
    config_json JSON NOT NULL,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT chk_pulse_runtime_singleton CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
INSERT IGNORE INTO pulse_runtime_config (id, revision, config_json) VALUES (1, 0, JSON_OBJECT());

CREATE TABLE IF NOT EXISTS pulse_runtime_role (
    role VARCHAR(16) NOT NULL,
    public_key VARBINARY(32) NOT NULL,
    environment_json JSON NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (role),
    CONSTRAINT chk_pulse_runtime_role CHECK (role IN ('api', 'worker'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS pulse_runtime_secret (
    secret_name VARCHAR(96) NOT NULL,
    role VARCHAR(16) NOT NULL,
    ciphertext BLOB NULL,
    fingerprint CHAR(64) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    PRIMARY KEY (secret_name),
    CONSTRAINT chk_pulse_runtime_secret_role CHECK (role IN ('api', 'worker'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- Durable mutation receipts retain only the redacted response and payload
-- digest. Replays do not write another audit event or advance the revision.
CREATE TABLE IF NOT EXISTS pulse_runtime_change (
    actor_id VARCHAR(128) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    response_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (actor_id, request_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

-- +goose Down
-- Keep encrypted operational state and mutation receipts during rollback.
-- Older binaries ignore these tables; dropping them could restore stale env
-- credentials or forget an already acknowledged settings mutation.
SELECT 1;
