-- +goose Up
-- One durable serialization row protects all content user-period and business-day caps.
-- Totals remain derived from pulse_content_award; this table stores no money.
CREATE TABLE pulse_content_award_limit_guard (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    scope_key VARCHAR(191) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uk_pulse_content_award_limit_guard_scope (scope_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO pulse_content_award_limit_guard (id, scope_key) VALUES (1, 'global');

-- +goose Down
DROP TABLE IF EXISTS pulse_content_award_limit_guard;
