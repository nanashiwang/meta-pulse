-- +goose Up
CREATE INDEX idx_reward_feed ON pulse_reward_grant(trigger_type, status, created_at, id);

-- +goose Down
DROP INDEX idx_reward_feed ON pulse_reward_grant;
