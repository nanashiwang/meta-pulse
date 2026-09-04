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
UPDATE pulse_settlement_outbox
SET status = 'dead'
WHERE status = 'conflict';
