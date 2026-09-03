-- +goose Up
-- Ledger payload fingerprints make idempotency conflict detection durable.
ALTER TABLE pulse_ledger_entry
    ADD COLUMN payload_hash CHAR(64) NULL AFTER idempotency_key;

-- Existing rows (if any) receive a deterministic migration fingerprint. New
-- writes must provide the real request fingerprint and satisfy NOT NULL below.
UPDATE pulse_ledger_entry
SET payload_hash = SHA2(CONCAT(
    user_id, ':', period_id, ':', asset_type, ':', operation, ':', amount, ':',
    source_type, ':', source_ref, ':', idempotency_key
), 256)
WHERE payload_hash IS NULL;

ALTER TABLE pulse_ledger_entry
    MODIFY COLUMN payload_hash CHAR(64) NOT NULL;

-- A ledger is append-only. The database guard protects the invariant even if
-- an operator or an ORM accidentally attempts to mutate accounting history.
-- +goose StatementBegin
CREATE TRIGGER trg_pulse_ledger_entry_no_update
BEFORE UPDATE ON pulse_ledger_entry
FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'pulse ledger is append-only';
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_pulse_ledger_entry_no_delete
BEFORE DELETE ON pulse_ledger_entry
FOR EACH ROW
BEGIN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'pulse ledger is append-only';
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS trg_pulse_ledger_entry_no_delete;
DROP TRIGGER IF EXISTS trg_pulse_ledger_entry_no_update;
ALTER TABLE pulse_ledger_entry DROP COLUMN payload_hash;
