-- +goose Up
-- Allow continuous rules to run the EXP pool by probability only. Reservation,
-- settlement and integer overflow bounds still apply; caps of old rows stay intact.
ALTER TABLE pulse_reward_budget DROP CHECK chk_pulse_budget_cap;
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_pulse_budget_cap CHECK (
 (unlimited = FALSE AND reserved_amount <= hard_cap - settled_amount) OR
 (unlimited = TRUE AND budget_type IN ('loyalty', 'community_exp') AND hard_cap = 0 AND reserved_amount <= 9223372036854775807 - settled_amount)
);

-- +goose Down
-- Refuse rollback while unlimited EXP budgets exist rather than silently impose a cap.
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_no_unlimited_exp_rollback CHECK (unlimited = FALSE OR budget_type = 'loyalty');
ALTER TABLE pulse_reward_budget DROP CHECK chk_pulse_budget_cap;
ALTER TABLE pulse_reward_budget ADD CONSTRAINT chk_pulse_budget_cap CHECK (
 (unlimited = FALSE AND reserved_amount <= hard_cap - settled_amount) OR
 (unlimited = TRUE AND budget_type = 'loyalty' AND hard_cap = 0 AND reserved_amount <= 9223372036854775807 - settled_amount)
);
ALTER TABLE pulse_reward_budget DROP CHECK chk_no_unlimited_exp_rollback;
