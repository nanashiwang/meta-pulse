// Package score computes the composite leaderboard score. Everything is
// integer arithmetic over basis points (0..10000) so a recomputation over the
// same inputs is bit-for-bit reproducible.
package score

import (
	"errors"
	"sort"
)

// Dimension groups benchmarks that measure one capability.
type Dimension string

const (
	Reasoning Dimension = "reasoning"
	Coding    Dimension = "coding"
	Agent     Dimension = "agent"
	// Other benchmarks are shown on model pages but do not feed the score.
	Other Dimension = "other"
)

const Full = 10000

// Method is one immutable, versioned scoring recipe. Changing weights or the
// coverage floor means publishing a new version, never editing an old one.
type Method struct {
	Version string
	// Weights in arbitrary positive integers; only their ratio matters.
	Weights []DimensionWeight
	// MinCoverageBps is the share of total weight a model must have data for
	// before it receives a composite score.
	MinCoverageBps int64
}

type DimensionWeight struct {
	Dimension Dimension
	Weight    int64
}

// V1 scores quality only. Price and speed stay separate columns so a cheap
// model is never ranked above a stronger one by an opaque trade-off.
var V1 = Method{
	Version: "v1",
	Weights: []DimensionWeight{
		{Reasoning, 40},
		{Coding, 35},
		{Agent, 25},
	},
	MinCoverageBps: 6000,
}

func (m Method) Validate() error {
	if m.Version == "" || len(m.Weights) == 0 || m.MinCoverageBps < 0 || m.MinCoverageBps > Full {
		return errors.New("invalid score method")
	}
	seen := map[Dimension]bool{}
	for _, w := range m.Weights {
		if w.Weight <= 0 || w.Dimension == Other || seen[w.Dimension] {
			return errors.New("invalid score method weight")
		}
		seen[w.Dimension] = true
	}
	return nil
}

// Benchmark describes how one benchmark's raw values are read.
type Benchmark struct {
	Slug           string
	Dimension      Dimension
	HigherIsBetter bool
}

// Input is one model with its latest raw value per benchmark slug.
type Input struct {
	Model   string
	Results map[string]int64
}

// Result is one model's score. Dimension and composite values are basis
// points; a missing key or nil Composite means "not enough data", never 0.
type Result struct {
	Model       string
	Dimensions  map[Dimension]int64
	Composite   *int64
	CoverageBps int64
}

// Compute normalises each benchmark across all models that report it
// (min-max to 0..10000), averages normalised values within a dimension, then
// weights the dimensions a model has data for. A benchmark that only one
// model reports, or where every model ties, carries no ranking signal and is
// skipped rather than awarding a full score.
func Compute(method Method, benchmarks []Benchmark, inputs []Input) ([]Result, error) {
	if err := method.Validate(); err != nil {
		return nil, err
	}
	weights := map[Dimension]int64{}
	var totalWeight int64
	for _, w := range method.Weights {
		weights[w.Dimension] = w.Weight
		totalWeight += w.Weight
	}

	type bounds struct{ min, max int64 }
	ranges := map[string]bounds{}
	meta := map[string]Benchmark{}
	for _, b := range benchmarks {
		meta[b.Slug] = b
		first := true
		var r bounds
		for _, in := range inputs {
			v, ok := in.Results[b.Slug]
			if !ok {
				continue
			}
			if first || v < r.min {
				r.min = v
			}
			if first || v > r.max {
				r.max = v
			}
			first = false
		}
		if !first && r.max > r.min {
			ranges[b.Slug] = r
		}
	}

	out := make([]Result, 0, len(inputs))
	for _, in := range inputs {
		sums := map[Dimension]int64{}
		counts := map[Dimension]int64{}
		// Iterate benchmarks in a fixed order so results never depend on map order.
		for _, b := range benchmarks {
			v, ok := in.Results[b.Slug]
			r, ranged := ranges[b.Slug]
			if !ok || !ranged || weights[b.Dimension] == 0 {
				continue
			}
			n := (v - r.min) * Full / (r.max - r.min)
			if !b.HigherIsBetter {
				n = Full - n
			}
			sums[b.Dimension] += n
			counts[b.Dimension]++
		}
		res := Result{Model: in.Model, Dimensions: map[Dimension]int64{}}
		var weighted, covered int64
		for _, w := range method.Weights {
			if counts[w.Dimension] == 0 {
				continue
			}
			d := sums[w.Dimension] / counts[w.Dimension]
			res.Dimensions[w.Dimension] = d
			weighted += d * w.Weight
			covered += w.Weight
		}
		res.CoverageBps = covered * Full / totalWeight
		if covered > 0 && res.CoverageBps >= method.MinCoverageBps {
			c := weighted / covered
			res.Composite = &c
		}
		out = append(out, res)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}
