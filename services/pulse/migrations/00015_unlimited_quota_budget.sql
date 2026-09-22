-- +goose Up
ALTER TABLE pulse_reward_budget ADD COLUMN unlimited BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE pulse_reward_budget DROP CHECK chk_pulse_budget_cap;
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_pulse_budget_cap CHECK (
 (unlimited = FALSE AND reserved_amount <= hard_cap - settled_amount) OR
 (unlimited = TRUE AND budget_type = 'loyalty' AND hard_cap = 0 AND reserved_amount <= 9223372036854775807 - settled_amount)
);
-- +goose StatementBegin
CREATE TRIGGER trg_budget_unlimited_frozen BEFORE UPDATE ON pulse_reward_budget FOR EACH ROW
BEGIN
 IF OLD.unlimited <> NEW.unlimited AND EXISTS (SELECT 1 FROM pulse_period WHERE id = OLD.period_id AND status <> 'draft') THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active quota cap policy is immutable';
 END IF;
END;
-- +goose StatementEnd

-- +goose Down
-- Refuse rollback when unlimited budgets exist rather than silently impose a cap.
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_no_unlimited_rollback CHECK (unlimited = FALSE);
DROP TRIGGER IF EXISTS trg_budget_unlimited_frozen;
ALTER TABLE pulse_reward_budget DROP CHECK chk_pulse_budget_cap;
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_pulse_budget_cap CHECK (reserved_amount <= hard_cap - settled_amount);
ALTER TABLE pulse_reward_budget DROP CHECK chk_no_unlimited_rollback;
ALTER TABLE pulse_reward_budget DROP COLUMN unlimited;
