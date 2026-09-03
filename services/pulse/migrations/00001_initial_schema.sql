-- +goose Up
-- Meta Pulse M0 core schema. All accounting values use integer/fixed-point
-- units; no FLOAT/DOUBLE column is allowed in the economic path.

CREATE TABLE pulse_period (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    period_key VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    starts_at DATETIME(6) NOT NULL,
    ends_at DATETIME(6) NOT NULL,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    config_version VARCHAR(64) NOT NULL,
    random_version VARCHAR(64) NOT NULL,
    period_secret_hash CHAR(64) NULL,
    activated_at DATETIME(6) NULL,
    settling_at DATETIME(6) NULL,
    closed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_period_key (period_key),
    KEY idx_pulse_period_status_time (status, starts_at, ends_at),
    CONSTRAINT chk_pulse_period_time CHECK (ends_at > starts_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_economics_rule (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    period_id BIGINT UNSIGNED NOT NULL,
    rule_key VARCHAR(128) NOT NULL,
    priority INT NOT NULL DEFAULT 0,
    model_pattern VARCHAR(191) NULL,
    channel_id BIGINT UNSIGNED NULL,
    eligible TINYINT(1) NOT NULL DEFAULT 1,
    multiplier_bps INT UNSIGNED NOT NULL,
    config_version VARCHAR(64) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_economics_rule (period_id, rule_key),
    KEY idx_pulse_economics_match (period_id, priority, channel_id),
    CONSTRAINT chk_pulse_economics_multiplier CHECK (multiplier_bps <= 1000000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_usage_event (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_system VARCHAR(64) NOT NULL,
    source_event_id VARCHAR(191) NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    source_created_at DATETIME(6) NOT NULL,
    quota_delta BIGINT NOT NULL,
    eligible TINYINT(1) NOT NULL,
    economics_rule_id BIGINT UNSIGNED NULL,
    multiplier_bps INT UNSIGNED NOT NULL,
    contribution_milli BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'accepted',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_usage_source (source_system, source_event_id),
    KEY idx_pulse_usage_user_period (user_id, period_id, source_created_at),
    KEY idx_pulse_usage_cursor (source_system, source_created_at, source_event_id),
    KEY idx_pulse_usage_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_ingest_conflict (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_system VARCHAR(64) NOT NULL,
    source_event_id VARCHAR(191) NOT NULL,
    existing_payload_hash CHAR(64) NOT NULL,
    incoming_payload_hash CHAR(64) NOT NULL,
    reason VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'open',
    resolution_reason VARCHAR(500) NULL,
    resolved_by VARCHAR(128) NULL,
    resolved_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_pulse_ingest_conflict_source (source_system, source_event_id),
    KEY idx_pulse_ingest_conflict_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_ledger_entry (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    asset_type VARCHAR(32) NOT NULL,
    operation VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    source_type VARCHAR(64) NOT NULL,
    source_ref VARCHAR(191) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    reversal_of_entry_id BIGINT UNSIGNED NULL,
    reason VARCHAR(500) NULL,
    metadata_json JSON NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_ledger_idempotency (operation, idempotency_key),
    KEY idx_pulse_ledger_account (user_id, period_id, asset_type, id),
    KEY idx_pulse_ledger_source (source_type, source_ref),
    KEY idx_pulse_ledger_reversal (reversal_of_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_account (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    asset_type VARCHAR(32) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_account (user_id, period_id, asset_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_reward_definition (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    period_id BIGINT UNSIGNED NOT NULL,
    reward_key VARCHAR(128) NOT NULL,
    reward_type VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    weight BIGINT UNSIGNED NOT NULL,
    transferable_quota TINYINT(1) NOT NULL DEFAULT 0,
    config_version VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_reward_definition (period_id, reward_key),
    CONSTRAINT chk_pulse_reward_amount CHECK (amount >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_reward_budget (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    period_id BIGINT UNSIGNED NOT NULL,
    budget_type VARCHAR(64) NOT NULL,
    hard_cap BIGINT NOT NULL,
    reserved_amount BIGINT NOT NULL DEFAULT 0,
    settled_amount BIGINT NOT NULL DEFAULT 0,
    released_amount BIGINT NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_reward_budget (period_id, budget_type),
    CONSTRAINT chk_pulse_budget_nonnegative CHECK (
        hard_cap >= 0 AND reserved_amount >= 0 AND settled_amount >= 0 AND released_amount >= 0
    ),
    CONSTRAINT chk_pulse_budget_cap CHECK (reserved_amount + settled_amount <= hard_cap)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_reward_grant (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    grant_id VARCHAR(64) NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    action_id VARCHAR(191) NOT NULL,
    trigger_type VARCHAR(32) NOT NULL,
    reward_definition_id BIGINT UNSIGNED NOT NULL,
    reward_type VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    random_value CHAR(64) NOT NULL,
    config_version VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    source_ref VARCHAR(191) NOT NULL,
    reason VARCHAR(255) NOT NULL,
    settled_at DATETIME(6) NULL,
    reversed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_reward_grant_id (grant_id),
    UNIQUE KEY uk_pulse_reward_action (period_id, user_id, action_id),
    UNIQUE KEY uk_pulse_reward_source_ref (source_ref),
    KEY idx_pulse_reward_status (status, created_at),
    KEY idx_pulse_reward_user (user_id, period_id, created_at),
    CONSTRAINT chk_pulse_grant_amount CHECK (amount >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_settlement_outbox (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    reward_grant_id BIGINT UNSIGNED NOT NULL,
    operation VARCHAR(32) NOT NULL DEFAULT 'grant',
    payload_hash CHAR(64) NOT NULL,
    payload_json JSON NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempts INT UNSIGNED NOT NULL DEFAULT 0,
    next_attempt_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    leased_until DATETIME(6) NULL,
    last_error VARCHAR(1000) NULL,
    completed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_settlement_grant (reward_grant_id),
    KEY idx_pulse_settlement_dispatch (status, next_attempt_at, leased_until)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_idempotency (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    scope VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    response_status INT UNSIGNED NULL,
    response_json JSON NULL,
    resource_type VARCHAR(64) NULL,
    resource_id VARCHAR(191) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_idempotency (scope, idempotency_key),
    KEY idx_pulse_idempotency_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_worker_cursor (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    cursor_name VARCHAR(128) NOT NULL,
    source_system VARCHAR(64) NOT NULL,
    cursor_value VARCHAR(191) NOT NULL,
    watermark_at DATETIME(6) NULL,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_worker_cursor (cursor_name, source_system)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_experiment_assignment (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    experiment_id VARCHAR(128) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    cohort VARCHAR(32) NOT NULL,
    bucket_bps INT UNSIGNED NOT NULL,
    assigned_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_experiment_assignment (experiment_id, user_id),
    CONSTRAINT chk_pulse_experiment_bucket CHECK (bucket_bps < 10000)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_user_period_stat (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    net_contribution_milli BIGINT NOT NULL DEFAULT 0,
    entitled_tickets BIGINT NOT NULL DEFAULT 0,
    spent_tickets BIGINT NOT NULL DEFAULT 0,
    usage_event_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_user_period_stat (user_id, period_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_metric_daily (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    metric_date DATE NOT NULL,
    metric_name VARCHAR(128) NOT NULL,
    dimension_hash CHAR(64) NOT NULL,
    dimensions_json JSON NULL,
    metric_value BIGINT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_metric_daily (metric_date, metric_name, dimension_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_audit_log (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_type VARCHAR(32) NOT NULL,
    actor_id VARCHAR(128) NOT NULL,
    action VARCHAR(128) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(191) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    before_json JSON NULL,
    after_json JSON NULL,
    request_id VARCHAR(128) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_pulse_audit_resource (resource_type, resource_id, created_at),
    KEY idx_pulse_audit_actor (actor_type, actor_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS pulse_audit_log;
DROP TABLE IF EXISTS pulse_metric_daily;
DROP TABLE IF EXISTS pulse_user_period_stat;
DROP TABLE IF EXISTS pulse_experiment_assignment;
DROP TABLE IF EXISTS pulse_worker_cursor;
DROP TABLE IF EXISTS pulse_idempotency;
DROP TABLE IF EXISTS pulse_settlement_outbox;
DROP TABLE IF EXISTS pulse_reward_grant;
DROP TABLE IF EXISTS pulse_reward_budget;
DROP TABLE IF EXISTS pulse_reward_definition;
DROP TABLE IF EXISTS pulse_account;
DROP TABLE IF EXISTS pulse_ledger_entry;
DROP TABLE IF EXISTS pulse_ingest_conflict;
DROP TABLE IF EXISTS pulse_usage_event;
DROP TABLE IF EXISTS pulse_economics_rule;
DROP TABLE IF EXISTS pulse_period;
