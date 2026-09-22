package ports

import (
	"math"
	"testing"
)

func TestUnlimitedQuotaAccountingBounds(t *testing.T) {
	b := RewardBudget{Unlimited: true, BudgetType: "loyalty", SettledAmount: 1000, ReservedAmount: 100}
	if !b.CanReserve(1000000) {
		t.Fatal("unlimited quota capped")
	}
	b.BudgetType = "community_exp"
	if b.CanReserve(1) {
		t.Fatal("EXP became unlimited")
	}
	b.BudgetType = "loyalty"
	b.HardCap = 10
	if b.CanReserve(1) {
		t.Fatal("ambiguous cap accepted")
	}
	b.HardCap = 0
	b.SettledAmount = math.MaxInt64 - 100
	if !b.CanReserve(0) || b.CanReserve(1) {
		t.Fatal("overflow boundary")
	}
	b.Unlimited = false
	b.HardCap = 100
	b.SettledAmount = 90
	b.ReservedAmount = 10
	if !b.CanReserve(0) || b.CanReserve(1) {
		t.Fatal("legacy cap bypassed")
	}
}
