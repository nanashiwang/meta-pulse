-- +goose Up
-- Cover the full SUM/COUNT reconciliation without a random primary-key lookup
-- for every historical entry. Keep idx_pulse_ledger_account for recent-ID reads.
ALTER TABLE pulse_ledger_entry
    ADD INDEX idx_pulse_ledger_reconcile (user_id, period_id, asset_type, amount),
    ALGORITHM=INPLACE, LOCK=NONE;

-- +goose Down
ALTER TABLE pulse_ledger_entry
    DROP INDEX idx_pulse_ledger_reconcile,
    ALGORITHM=INPLACE, LOCK=NONE;
