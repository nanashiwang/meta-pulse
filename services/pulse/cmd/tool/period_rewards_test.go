package main

import "testing"

func TestParsePeriodRewardsRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{}`, `[{"key":"a","amount":5,"weight":1,"user_id":7}]`, `[{"key":"a","amount":5,"amount":6,"weight":1}]`, `[{"key":"a","amount":5.5,"weight":1}]`, `[{"key":"a","amount":5,"weight":-1}]`, `[{"key":"a","amount":5,"weight":1}] {}`, `[{"key":"a","amount":5}]`, `[{"key":"a","amount":9223372036854775808,"weight":1}]`} {
		if _, err := parsePeriodRewards([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	got, err := parsePeriodRewards([]byte(`[{"key":"small","amount":5,"weight":99},{"weight":1,"amount":20,"key":"large"}]`))
	if err != nil || len(got) != 2 || got[1].Amount != 20 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
