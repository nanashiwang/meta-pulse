-- +goose Up
CREATE INDEX idx_ticket_rule_fifo ON pulse_ticket_lot(user_id, period_id, earned_at, id);

-- +goose Down
DROP INDEX idx_ticket_rule_fifo ON pulse_ticket_lot;
