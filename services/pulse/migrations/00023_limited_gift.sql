-- +goose Up
CREATE TABLE pulse_gift_usage(user_id BIGINT UNSIGNED NOT NULL, day DATE NOT NULL, recorded BIGINT NULL, ready BOOLEAN NOT NULL DEFAULT 0, verified_at BIGINT NOT NULL DEFAULT 0, retry_at BIGINT NOT NULL DEFAULT 0, PRIMARY KEY(user_id,day), INDEX gift_usage_queue(day,ready,retry_at)) ENGINE=InnoDB;
CREATE TABLE pulse_gift_delivery(round_id BIGINT UNSIGNED NOT NULL,user_id BIGINT UNSIGNED NOT NULL,grant_id VARCHAR(64) COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '',tickets BIGINT NOT NULL DEFAULT 0, PRIMARY KEY(round_id,user_id)) ENGINE=InnoDB;
CREATE TABLE pulse_gift_order(round_id BIGINT UNSIGNED NOT NULL,user_id BIGINT UNSIGNED NOT NULL,forum_user BIGINT UNSIGNED NOT NULL,actor BIGINT UNSIGNED NOT NULL,flash BIGINT NOT NULL,tickets BIGINT NOT NULL,done BOOLEAN NOT NULL DEFAULT 0,retry_at BIGINT NOT NULL DEFAULT 0,PRIMARY KEY(round_id,user_id),UNIQUE KEY(round_id,forum_user),INDEX gift_jobs(done,retry_at)) ENGINE=InnoDB;
-- +goose Down
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Gift receipts must be retained';
