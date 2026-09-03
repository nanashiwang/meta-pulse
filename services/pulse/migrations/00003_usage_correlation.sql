-- +goose Up
-- Keep the minimum safe, non-sensitive correlation fields needed to explain
-- rule matching and to resolve task/refund relationships. Prompt, response,
-- IP, token and cookie data remain outside Pulse.
ALTER TABLE pulse_usage_event
    ADD COLUMN model_name VARCHAR(191) NOT NULL DEFAULT '' AFTER status,
    ADD COLUMN channel_id BIGINT UNSIGNED NULL AFTER model_name,
    ADD COLUMN request_id VARCHAR(191) NOT NULL DEFAULT '' AFTER channel_id,
    ADD COLUMN related_source_event_id VARCHAR(191) NOT NULL DEFAULT '' AFTER request_id,
    ADD COLUMN review_reason VARCHAR(500) NOT NULL DEFAULT '' AFTER related_source_event_id;

ALTER TABLE pulse_ingest_conflict
    ADD UNIQUE KEY uk_pulse_ingest_conflict_payload (
        source_system, source_event_id, incoming_payload_hash
    );

-- +goose Down
ALTER TABLE pulse_ingest_conflict
    DROP INDEX uk_pulse_ingest_conflict_payload;
ALTER TABLE pulse_usage_event
    DROP COLUMN review_reason,
    DROP COLUMN related_source_event_id,
    DROP COLUMN request_id,
    DROP COLUMN channel_id,
    DROP COLUMN model_name;
