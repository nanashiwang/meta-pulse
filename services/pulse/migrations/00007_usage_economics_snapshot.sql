-- +goose Up
-- Preserve the immutable economics configuration version applied to each
-- usage event. Rule id and multiplier alone are insufficient audit data if
-- operators later compare or migrate versioned rule sets.
ALTER TABLE pulse_usage_event
    ADD COLUMN economics_config_version VARCHAR(64) NULL AFTER economics_rule_id;

UPDATE pulse_usage_event AS usage_event
JOIN pulse_economics_rule AS economics_rule
  ON economics_rule.id = usage_event.economics_rule_id
SET usage_event.economics_config_version = economics_rule.config_version
WHERE usage_event.economics_rule_id IS NOT NULL;

UPDATE pulse_usage_event
SET economics_config_version = ''
WHERE economics_config_version IS NULL;

ALTER TABLE pulse_usage_event
    MODIFY COLUMN economics_config_version VARCHAR(64) NOT NULL DEFAULT '',
    ADD CONSTRAINT chk_pulse_usage_economics_snapshot CHECK (
        economics_rule_id IS NULL OR economics_config_version <> ''
    );

-- +goose Down
ALTER TABLE pulse_usage_event
    DROP CHECK chk_pulse_usage_economics_snapshot,
    DROP COLUMN economics_config_version;
