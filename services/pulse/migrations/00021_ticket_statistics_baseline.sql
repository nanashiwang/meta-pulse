-- +goose Up
-- Display-only correction authorized for historical erroneous grants/resets.
-- Never rewrite ledger entries, balances, ticket lots or reward grants.
CREATE TABLE IF NOT EXISTS pulse_ticket_statistics_baseline (
 user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 total_offset BIGINT NOT NULL,
 removed_offset BIGINT NOT NULL,
 original_total BIGINT NOT NULL,
 original_spent BIGINT NOT NULL,
 original_removed BIGINT NOT NULL,
 corrected_at DATETIME(6) NOT NULL,
 reason VARCHAR(255) NOT NULL
) ENGINE=InnoDB;

-- One atomic snapshot; a retry preserves previously captured offsets.
INSERT IGNORE INTO pulse_ticket_statistics_baseline
 (user_id,total_offset,removed_offset,original_total,original_spent,original_removed,corrected_at,reason)
SELECT user_id,
 SUM(CASE WHEN amount>0 AND operation IN ('ticket_mint','ticket_bonus','ticket_adjustment') THEN amount ELSE 0 END)
 +SUM(CASE WHEN operation='ticket_spend' THEN amount ELSE 0 END),
 -SUM(CASE WHEN amount<0 AND operation IN ('ticket_adjustment','ticket_reverse','ticket_expire') THEN amount ELSE 0 END),
 SUM(CASE WHEN amount>0 AND operation IN ('ticket_mint','ticket_bonus','ticket_adjustment') THEN amount ELSE 0 END),
 -SUM(CASE WHEN operation='ticket_spend' THEN amount ELSE 0 END),
 -SUM(CASE WHEN amount<0 AND operation IN ('ticket_adjustment','ticket_reverse','ticket_expire') THEN amount ELSE 0 END),
 NOW(6),'2026-11-13: reset display totals to spent and removed to zero; preserve ledger'
FROM pulse_ledger_entry WHERE asset_type='ticket' GROUP BY user_id;

-- +goose Down
-- Preserve the audit snapshot across rollback/re-upgrade; old code ignores it.
SELECT 1;
