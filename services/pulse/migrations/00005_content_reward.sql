-- +goose Up
-- M7 community content candidates/awards. The forum remains a separate
-- database and is only read by the content ingest adapter.
CREATE TABLE pulse_content_candidate (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    source_system VARCHAR(64) NOT NULL,
    source_content_id VARCHAR(191) NOT NULL,
    content_type VARCHAR(64) NOT NULL,
    author_user_id BIGINT UNSIGNED NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    title VARCHAR(500) NOT NULL DEFAULT '',
    source_created_at DATETIME(6) NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    cursor_value VARCHAR(191) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    review_actor_type VARCHAR(32) NULL,
    review_actor_id VARCHAR(128) NULL,
    review_reason VARCHAR(500) NULL,
    reviewed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_content_candidate_source (source_system, source_content_id),
    KEY idx_pulse_content_candidate_review (status, created_at),
    KEY idx_pulse_content_candidate_author (author_user_id, period_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE pulse_content_award (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    candidate_id BIGINT UNSIGNED NOT NULL,
    award_version BIGINT UNSIGNED NOT NULL,
    action_id VARCHAR(191) NOT NULL,
    period_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    amount BIGINT NOT NULL,
    reward_type VARCHAR(64) NOT NULL,
    budget_type VARCHAR(64) NOT NULL DEFAULT 'content_reward',
    grant_id VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_content_award_action (action_id),
    UNIQUE KEY uk_pulse_content_award_version (candidate_id, award_version),
    KEY idx_pulse_content_award_user_period (user_id, period_id, status),
    KEY idx_pulse_content_award_day (created_at, status),
    CONSTRAINT chk_pulse_content_award_amount CHECK (amount >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE IF EXISTS pulse_content_award;
DROP TABLE IF EXISTS pulse_content_candidate;
