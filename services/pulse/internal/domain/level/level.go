// Package level calculates a stable lifetime level from contribution history.
package level

import (
	"sort"

	"github.com/nanashiwang/meta-pulse/internal/domain/money"
)

type Definition struct {
	Key                  string
	Name                 string
	MinContributionMilli money.Milli
}

type Result struct {
	Key                  string
	Name                 string
	LifetimeContribution money.Milli
}

func Calculate(lifetime money.Milli, definitions []Definition) Result {
	result := Result{LifetimeContribution: lifetime}
	if len(definitions) == 0 {
		return result
	}
	ordered := append([]Definition(nil), definitions...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].MinContributionMilli < ordered[j].MinContributionMilli })
	for _, definition := range ordered {
		if lifetime >= definition.MinContributionMilli {
			result.Key, result.Name = definition.Key, definition.Name
		}
	}
	return result
}
