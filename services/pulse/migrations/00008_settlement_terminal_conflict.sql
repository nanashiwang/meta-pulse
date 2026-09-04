-- +goose Up
-- Separate terminal integrity failures from retry-exhausted dead rows. Only
-- dead rows remain eligible for automatic Query/Reconciliation.
UPDATE pulse_settlement_outbox
SET status = 'conflict'
WHERE status = 'dead'
  AND (
    last_error LIKE 'invalid settlement payload%'
    OR last_error LIKE 'settlement source conflict%'
  );

-- +goose Down
-- This data classification is intentionally irreversible. Turning every
-- conflict back into dead would make terminal integrity failures eligible for
-- automatic Query/Reconciliation and could settle an unverified payload.
SELECT 1;
