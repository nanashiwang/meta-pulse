package economics

import (
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
)

func TestSelectPrefersSpecificRule(t *testing.T) {
	channel := uint64(3)
	rule, ok := Select([]Rule{
		{Key: "default", Priority: 10, MultiplierBps: money.BpsOne},
		{Key: "model-channel", Priority: 10, ModelPattern: "gpt-*", ChannelID: &channel, MultiplierBps: 12000},
	}, "gpt-4o", channel)
	if !ok || rule.Key != "model-channel" {
		t.Fatalf("rule = %+v, found = %v", rule, ok)
	}
}

func TestEvaluatePreservesNegativeRefund(t *testing.T) {
	decision, err := Evaluate(-1000, Rule{ID: 1, Key: "refund", Eligible: true, MultiplierBps: 12000})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Contribution != -1200 {
		t.Fatalf("contribution = %d, want -1200", decision.Contribution)
	}
}

func TestIneligibleRuleProducesNoContribution(t *testing.T) {
	decision, err := Evaluate(1000, Rule{Key: "blocked", MultiplierBps: money.BpsOne})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Eligible || decision.Contribution != 0 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestValidateRulesFailsClosed(t *testing.T) {
	valid := Rule{ID: 1, Key: "default", MultiplierBps: 10000, ConfigVersion: "v1"}
	if err := ValidateRules([]Rule{valid}, "v1"); err != nil {
		t.Fatal(err)
	}
	cases := []Rule{
		{ID: 1, Key: "default", MultiplierBps: 10000, ConfigVersion: "v2"},
		{ID: 1, Key: "default", ModelPattern: "[", MultiplierBps: 10000, ConfigVersion: "v1"},
		{ID: 1, Key: "default", MultiplierBps: -1, ConfigVersion: "v1"},
	}
	for _, rule := range cases {
		if err := ValidateRules([]Rule{rule}, "v1"); err == nil {
			t.Fatalf("invalid rule accepted: %+v", rule)
		}
	}
}
