-- +goose Up
ALTER TABLE pulse_period ADD COLUMN continuous BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN quota_validity_days INT NOT NULL DEFAULT 0;
CREATE TABLE pulse_ticket_user_lock (user_id BIGINT UNSIGNED PRIMARY KEY) ENGINE=InnoDB;
CREATE TABLE pulse_ticket_lot (
 id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
 user_id BIGINT UNSIGNED NOT NULL,
 period_id BIGINT UNSIGNED NOT NULL,
 mint_entry_id BIGINT UNSIGNED NOT NULL UNIQUE,
 issued BIGINT NOT NULL,
 remaining BIGINT NOT NULL,
 earned_at BIGINT NOT NULL,
 quota_expires_at BIGINT NOT NULL,
 INDEX idx_ticket_fifo (user_id, earned_at, id),
 CHECK (issued > 0 AND remaining >= 0 AND remaining <= issued)
) ENGINE=InnoDB;
CREATE TABLE pulse_ticket_allocation (
 spend_entry_id BIGINT UNSIGNED PRIMARY KEY,
 lot_id BIGINT UNSIGNED NOT NULL,
 INDEX idx_ticket_allocation_lot (lot_id)
) ENGINE=InnoDB;
-- +goose StatementBegin
CREATE TRIGGER trg_continuous_config_frozen BEFORE UPDATE ON pulse_period FOR EACH ROW
BEGIN
 IF (OLD.status <> 'draft' OR NEW.status <> 'draft') AND (OLD.continuous <> NEW.continuous OR OLD.quota_validity_days <> NEW.quota_validity_days) THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket rules are immutable';
 END IF;
 IF OLD.continuous AND NEW.status IN ('settling','closed') THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'continuous tickets do not expire at period close';
 END IF;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_ticket_lot_frozen BEFORE UPDATE ON pulse_ticket_lot FOR EACH ROW
BEGIN
 IF OLD.user_id <> NEW.user_id OR OLD.period_id <> NEW.period_id OR OLD.mint_entry_id <> NEW.mint_entry_id OR OLD.issued <> NEW.issued OR OLD.earned_at <> NEW.earned_at OR OLD.quota_expires_at <> NEW.quota_expires_at THEN
  SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket lot issuance is immutable';
 END IF;
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_ticket_allocation_append_update BEFORE UPDATE ON pulse_ticket_allocation FOR EACH ROW
BEGIN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket allocations are append only';
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_ticket_allocation_append_delete BEFORE DELETE ON pulse_ticket_allocation FOR EACH ROW
BEGIN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket allocations are append only';
END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER trg_ticket_lot_append_delete BEFORE DELETE ON pulse_ticket_lot FOR EACH ROW
BEGIN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'ticket issuance cannot be deleted';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_ticket_lot_append_delete;
DROP TRIGGER IF EXISTS trg_ticket_allocation_append_delete;
DROP TRIGGER IF EXISTS trg_ticket_allocation_append_update;
DROP TRIGGER IF EXISTS trg_ticket_lot_frozen;
DROP TRIGGER IF EXISTS trg_continuous_config_frozen;
DROP TABLE pulse_ticket_allocation;
DROP TABLE pulse_ticket_lot;
DROP TABLE pulse_ticket_user_lock;
ALTER TABLE pulse_period DROP COLUMN continuous, DROP COLUMN quota_validity_days;
