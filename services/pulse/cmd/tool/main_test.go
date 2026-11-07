package main

import "testing"

func TestParseRewardRetryArgsRequiresTargetGrant(t *testing.T) {
	grantID, err := parseRewardRetryArgs([]string{"--grant-id", " pg_test "})
	if err != nil || grantID != "pg_test" {
		t.Fatalf("grantID=%q err=%v", grantID, err)
	}
	for _, args := range [][]string{nil, {"--grant-id", " "}, {"--unknown"}} {
		if _, err := parseRewardRetryArgs(args); err == nil {
			t.Fatalf("args=%v were accepted", args)
		}
	}
}

func TestRetiredUsageCommands(t *testing.T) {
	for _, command := range []string{"backfill", "backtest", "cursor-seek", "ingest-snapshot", "access-check"} {
		if !isRetiredUsageCommand(command) {
			t.Fatalf("usage command %s accepted", command)
		}
	}
	for _, command := range []string{"reconcile", "reward-retry", "period-close", "ledger-check", "config-check", "migrate-up"} {
		if isRetiredUsageCommand(command) {
			t.Fatalf("retirement disabled recovery command %s", command)
		}
	}
}
