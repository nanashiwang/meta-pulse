-- +goose Up
-- Permanent rule-publication mutex. Keep existing identity and payload intact.
INSERT INTO pulse_idempotency (scope, idempotency_key, payload_hash)
VALUES ('period_create_lock', 'global', '447cc9dbdc73a33ea5be9cef405e81ef56c4e23b9202238aee120a0078c2fb2a')
ON DUPLICATE KEY UPDATE id = id;

-- +goose Down
-- Keep the mutex: older code also uses this identity and can safely reuse it.
SELECT 1;
