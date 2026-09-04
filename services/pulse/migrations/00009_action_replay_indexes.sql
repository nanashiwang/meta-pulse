-- +goose Up
-- Compatibility lookups preserve pre-upgrade responses without scanning all history.
ALTER TABLE pulse_idempotency ADD INDEX idx_idempotency_key_scope (idempotency_key, scope);
ALTER TABLE pulse_reward_grant ADD INDEX idx_grant_user_action_trigger (user_id, action_id, trigger_type);

-- +goose Down
ALTER TABLE pulse_reward_grant DROP INDEX idx_grant_user_action_trigger;
ALTER TABLE pulse_idempotency DROP INDEX idx_idempotency_key_scope;
