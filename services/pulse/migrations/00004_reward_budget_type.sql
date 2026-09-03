-- +goose Up
-- A grant must retain the immutable budget it reserved so settlement and
-- rollback can never accidentally touch another campaign budget.
ALTER TABLE pulse_reward_grant
    ADD COLUMN budget_type VARCHAR(64) NOT NULL DEFAULT 'loyalty' AFTER reason;

-- +goose Down
ALTER TABLE pulse_reward_grant DROP COLUMN budget_type;
