package usage

import "testing"

func TestEventTypesAreStable(t *testing.T) {
	if EventConsume != "consume" || EventRefund != "refund" || EventCorrect != "correction" {
		t.Fatal("usage event type changed")
	}
}
