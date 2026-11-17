-- +goose Up
ALTER TABLE pulse_reward_grant
    ADD COLUMN bonus_experience BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_experience_status VARCHAR(24) NOT NULL DEFAULT '',
    ADD INDEX idx_pulse_bonus_delivery (user_id, bonus_experience_status, id),
    ADD CONSTRAINT chk_pulse_bonus_experience CHECK (
        (bonus_experience = 0 AND bonus_experience_status = '') OR
        (bonus_experience BETWEEN 1 AND 1000000 AND trigger_type = 'pulse' AND reward_type = 'newapi_quota'
            AND bonus_experience_status IN ('pending', 'settled', 'shadow', 'reversal_pending', 'reversed'))
    );

-- +goose Down
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Draw bonus receipts and delivery state cannot be removed';
