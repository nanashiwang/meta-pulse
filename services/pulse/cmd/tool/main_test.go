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
