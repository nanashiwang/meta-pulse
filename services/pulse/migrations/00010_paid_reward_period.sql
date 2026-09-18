-- +goose Up
ALTER TABLE pulse_period ADD COLUMN funding_policy VARCHAR(32) NOT NULL DEFAULT 'legacy', ADD COLUMN ticket_threshold_milli BIGINT NOT NULL DEFAULT 0;
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_period_reward_config_frozen BEFORE UPDATE ON pulse_period FOR EACH ROW
BEGIN
 IF OLD.status <> 'draft' AND NEW.status = 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active period cannot return to draft';
 END IF;
 IF (OLD.status <> 'draft' OR NEW.status <> 'draft') AND (NEW.funding_policy <> OLD.funding_policy OR NEW.ticket_threshold_milli <> OLD.ticket_threshold_milli OR NEW.starts_at <> OLD.starts_at OR NEW.ends_at <> OLD.ends_at OR NEW.config_version <> OLD.config_version OR NEW.random_version <> OLD.random_version OR NEW.timezone <> OLD.timezone) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active period reward configuration is immutable';
 END IF;
END;
-- +goose StatementEnd
-- +goose Down
DROP TRIGGER IF EXISTS trg_pulse_period_reward_config_frozen;
ALTER TABLE pulse_period DROP COLUMN funding_policy, DROP COLUMN ticket_threshold_milli;
