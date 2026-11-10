-- +goose Up
-- Cover per-user ticket statistics without reading historical contribution rows.
CREATE INDEX idx_pulse_ledger_user_asset_operation_amount
 ON pulse_ledger_entry (user_id, asset_type, operation, amount);

-- +goose Down
DROP INDEX idx_pulse_ledger_user_asset_operation_amount ON pulse_ledger_entry;
