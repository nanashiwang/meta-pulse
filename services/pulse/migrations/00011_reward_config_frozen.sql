-- +goose Up
-- Only draft periods accept reward configuration. Runtime budget counters
-- remain mutable through the existing transactional reservation/settlement path.
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_definition_draft_insert BEFORE INSERT ON pulse_reward_definition FOR EACH ROW
BEGIN
 IF COALESCE((SELECT status FROM pulse_period WHERE id = NEW.period_id FOR SHARE), '') <> 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reward definitions require a draft period';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_definition_frozen_update BEFORE UPDATE ON pulse_reward_definition FOR EACH ROW
BEGIN
 IF COALESCE((SELECT status FROM pulse_period WHERE id = OLD.period_id FOR SHARE), '') <> 'draft' OR COALESCE((SELECT status FROM pulse_period WHERE id = NEW.period_id FOR SHARE), '') <> 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active reward definitions are immutable';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_definition_frozen_delete BEFORE DELETE ON pulse_reward_definition FOR EACH ROW
BEGIN
 IF COALESCE((SELECT status FROM pulse_period WHERE id = OLD.period_id FOR SHARE), '') <> 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active reward definitions cannot be deleted';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_budget_draft_insert BEFORE INSERT ON pulse_reward_budget FOR EACH ROW
BEGIN
 IF COALESCE((SELECT status FROM pulse_period WHERE id = NEW.period_id FOR SHARE), '') <> 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'reward budgets require a draft period';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_budget_frozen_update BEFORE UPDATE ON pulse_reward_budget FOR EACH ROW
BEGIN
 IF (NOT (OLD.id <=> NEW.id) OR NOT (OLD.period_id <=> NEW.period_id) OR NOT (OLD.budget_type <=> NEW.budget_type) OR NOT (OLD.hard_cap <=> NEW.hard_cap)) AND
  (COALESCE((SELECT status FROM pulse_period WHERE id = OLD.period_id FOR SHARE), '') <> 'draft' OR COALESCE((SELECT status FROM pulse_period WHERE id = NEW.period_id FOR SHARE), '') <> 'draft') THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active reward budget configuration is immutable';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_reward_budget_frozen_delete BEFORE DELETE ON pulse_reward_budget FOR EACH ROW
BEGIN
 IF COALESCE((SELECT status FROM pulse_period WHERE id = OLD.period_id FOR SHARE), '') <> 'draft' THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'active reward budgets cannot be deleted';
 END IF;
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_pulse_reward_budget_frozen_delete;
DROP TRIGGER IF EXISTS trg_pulse_reward_budget_frozen_update;
DROP TRIGGER IF EXISTS trg_pulse_reward_budget_draft_insert;
DROP TRIGGER IF EXISTS trg_pulse_reward_definition_frozen_delete;
DROP TRIGGER IF EXISTS trg_pulse_reward_definition_frozen_update;
DROP TRIGGER IF EXISTS trg_pulse_reward_definition_draft_insert;
