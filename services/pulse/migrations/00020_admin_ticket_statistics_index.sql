-- +goose Up
-- MySQL DDL may complete after a client timeout, before Goose records success.
-- Skip only the exact completed index, so retry repairs that partial migration.
SET @pulse_admin_statistics_ddl = IF(
 (SELECT GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',')
  FROM information_schema.statistics
  WHERE table_schema=DATABASE() AND table_name='pulse_ledger_entry'
    AND index_name='idx_pulse_ledger_user_asset_operation_amount') = 'user_id,asset_type,operation,amount',
 'SELECT 1',
 'ALTER TABLE pulse_ledger_entry ADD INDEX idx_pulse_ledger_user_asset_operation_amount (user_id, asset_type, operation, amount), ALGORITHM=INPLACE, LOCK=NONE'
);
PREPARE pulse_admin_statistics_stmt FROM @pulse_admin_statistics_ddl;
EXECUTE pulse_admin_statistics_stmt;
DEALLOCATE PREPARE pulse_admin_statistics_stmt;

-- +goose Down
DROP INDEX idx_pulse_ledger_user_asset_operation_amount ON pulse_ledger_entry;
