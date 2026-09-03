package level

import (
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
)

func TestCalculateChoosesHighestReachedThreshold(t *testing.T) {
	result := Calculate(2500, []Definition{{Key: "new", Name: "新手", MinContributionMilli: 0}, {Key: "pulse", Name: "脉冲者", MinContributionMilli: 2000}})
	if result.Key != "pulse" || result.Name != "脉冲者" {
		t.Fatalf("result = %+v", result)
	}
	if result.LifetimeContribution != money.Milli(2500) {
		t.Fatalf("lifetime = %d", result.LifetimeContribution)
	}
}
