package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
)

// Extends the real-MySQL concurrency fixture with response loss at a boundary
// and an upgrade from the original, period-scoped idempotency schema.
func assertMySQLActionPeriodReplay(t *testing.T, db *sql.DB, action *ActionService, command ActionCommand, first ActionResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	mustExec := func(query string, args ...any) sql.Result {
		t.Helper()
		r, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	mustExec(`UPDATE pulse_period SET status='closed' WHERE id=?`, first.PeriodID)
	for i := 0; i < 100; i++ {
		got, err := action.Execute(ctx, command)
		if err != nil || got != first {
			t.Fatalf("closed replay %d: got=%+v err=%v", i, got, err)
		}
	}
	now := time.Now()
	r := mustExec(`INSERT INTO pulse_period (period_key,status,starts_at,ends_at,timezone,config_version,random_version) VALUES (?,'active',?,?,'Asia/Shanghai','replay-v2','hmac-v1')`, fmt.Sprintf("integration-replay-%d", now.UnixNano()), now.Add(-time.Minute), now.Add(time.Hour))
	nextID, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`UPDATE pulse_period SET status='closed' WHERE id=?`, nextID) })
	mustExec(`INSERT INTO pulse_reward_definition (period_id,reward_key,reward_type,amount,weight,transferable_quota,config_version,enabled) VALUES (?,'replay-quota','quota',7,1,0,'replay-v2',1)`, nextID)
	mustExec(`INSERT INTO pulse_reward_budget (period_id,budget_type,hard_cap,reserved_amount,settled_amount,released_amount,version) VALUES (?,'loyalty',100,0,0,0,0)`, nextID)
	mustExec(`INSERT INTO pulse_account (user_id,period_id,asset_type,balance,version) VALUES (?,?,'ticket',1,1)`, command.UserID, nextID)
	mustExec(`INSERT INTO pulse_ledger_entry (user_id,period_id,asset_type,operation,amount,balance_after,source_type,source_ref,idempotency_key,payload_hash,reason) VALUES (?,?,'ticket','ticket_mint',1,1,'integration',?,?,?,'replay fixture')`, command.UserID, nextID, fmt.Sprint(nextID), fmt.Sprintf("replay-mint:%d", nextID), fmt.Sprintf("%064d", 1))
	got, err := action.Execute(ctx, command)
	if err != nil || got != first {
		t.Fatalf("new period replay: got=%+v err=%v", got, err)
	}

	// Emulate a database written by the old binary. Only derived idempotency
	// indexes change; the original immutable Grant, Outbox and Ledger stay put.
	mustExec(`DELETE FROM pulse_idempotency WHERE scope IN (?,?)`, fmt.Sprintf("pulse_action_request:%d", command.UserID), fmt.Sprintf("pulse_action_identity:%d", command.UserID))
	response, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO pulse_idempotency (scope,idempotency_key,payload_hash,response_status,response_json,resource_type,resource_id) VALUES (?,?,?,201,?,'reward_grant',?)`, fmt.Sprintf("pulse_action:%d:%d", first.PeriodID, command.UserID), command.IdempotencyKey, actionPayloadHash(command, first.PeriodID, first.ConfigVersion), response, first.GrantID)
	got, err = action.Execute(ctx, command)
	if err != nil || got != first {
		t.Fatalf("legacy replay: got=%+v err=%v", got, err)
	}
	changed := command
	changed.ActionID = "different-action"
	if _, err := action.Execute(ctx, changed); !errors.Is(err, ledger.ErrIdempotencyConflict) {
		t.Fatalf("legacy key payload change err=%v", err)
	}

	// Independent request keys racing on one action must still converge.
	const n = 100
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			alias := command
			alias.IdempotencyKey = fmt.Sprintf("boundary-alias-%d", i)
			got, err := action.Execute(ctx, alias)
			if err != nil {
				errs[i] = err
			} else if got != first {
				errs[i] = fmt.Errorf("different response: %+v", got)
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("alias %d: %v", i, err)
		}
	}
	var grants, spends, outboxes, balance, reserved int
	for _, check := range []struct {
		query string
		dest  *int
		args  []any
	}{
		{`SELECT COUNT(*) FROM pulse_reward_grant WHERE user_id=? AND action_id=?`, &grants, []any{command.UserID, command.ActionID}},
		{`SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND operation='ticket_spend' AND period_id IN (?,?)`, &spends, []any{command.UserID, first.PeriodID, nextID}},
		{`SELECT COUNT(*) FROM pulse_settlement_outbox WHERE reward_grant_id IN (SELECT id FROM pulse_reward_grant WHERE user_id=? AND action_id=?)`, &outboxes, []any{command.UserID, command.ActionID}},
		{`SELECT balance FROM pulse_account WHERE user_id=? AND period_id=? AND asset_type='ticket'`, &balance, []any{command.UserID, nextID}},
		{`SELECT reserved_amount FROM pulse_reward_budget WHERE period_id=? AND budget_type='loyalty'`, &reserved, []any{nextID}},
	} {
		if err := db.QueryRowContext(ctx, check.query, check.args...).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	if grants != 1 || spends != 1 || outboxes != 1 || balance != 1 || reserved != 0 {
		t.Fatalf("cross-period invariant: grants=%d spends=%d outboxes=%d next tickets=%d reserved=%d", grants, spends, outboxes, balance, reserved)
	}
}
