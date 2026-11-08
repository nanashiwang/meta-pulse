package score

import "testing"

var benches = []Benchmark{
	{Slug: "r1", Dimension: Reasoning, HigherIsBetter: true},
	{Slug: "c1", Dimension: Coding, HigherIsBetter: true},
	{Slug: "a1", Dimension: Agent, HigherIsBetter: true},
	{Slug: "lat", Dimension: Agent, HigherIsBetter: false},
	{Slug: "misc", Dimension: Other, HigherIsBetter: true},
}

func byModel(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.Model] = r
	}
	return m
}

func TestComputeNormalisesAndWeights(t *testing.T) {
	rs, err := Compute(V1, benches, []Input{
		{Model: "a", Results: map[string]int64{"r1": 9000, "c1": 8000, "a1": 7000, "misc": 1}},
		{Model: "b", Results: map[string]int64{"r1": 5000, "c1": 6000, "a1": 3000, "misc": 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := byModel(rs)
	if *m["a"].Composite != Full || *m["b"].Composite != 0 {
		t.Fatalf("composites: a=%d b=%d", *m["a"].Composite, *m["b"].Composite)
	}
	if m["a"].CoverageBps != Full {
		t.Fatalf("coverage %d", m["a"].CoverageBps)
	}
	if _, ok := m["a"].Dimensions[Other]; ok {
		t.Fatal("other dimension must not be scored")
	}
}

func TestLowerIsBetterInverts(t *testing.T) {
	rs, _ := Compute(V1, benches, []Input{
		{Model: "fast", Results: map[string]int64{"lat": 100}},
		{Model: "slow", Results: map[string]int64{"lat": 900}},
	})
	m := byModel(rs)
	if m["fast"].Dimensions[Agent] != Full || m["slow"].Dimensions[Agent] != 0 {
		t.Fatalf("agent dims: %+v %+v", m["fast"].Dimensions, m["slow"].Dimensions)
	}
}

func TestInsufficientCoverageHasNoComposite(t *testing.T) {
	rs, _ := Compute(V1, benches, []Input{
		{Model: "a", Results: map[string]int64{"r1": 9000, "c1": 9000}},
		{Model: "b", Results: map[string]int64{"r1": 1000}},
		{Model: "c", Results: map[string]int64{"c1": 1000}},
	})
	m := byModel(rs)
	if m["a"].Composite == nil {
		t.Fatal("75% coverage should score")
	}
	if m["b"].Composite != nil || m["b"].CoverageBps != 4000 {
		t.Fatalf("b should be insufficient, got %+v", m["b"])
	}
	if _, ok := m["b"].Dimensions[Coding]; ok {
		t.Fatal("missing dimension must stay absent, not zero")
	}
}

func TestSingleReporterOrTieCarriesNoSignal(t *testing.T) {
	rs, _ := Compute(V1, benches, []Input{
		{Model: "a", Results: map[string]int64{"r1": 9000, "c1": 5000}},
		{Model: "b", Results: map[string]int64{"c1": 5000}},
	})
	m := byModel(rs)
	if len(m["a"].Dimensions) != 0 || m["a"].Composite != nil {
		t.Fatalf("no ranged benchmark should yield no score: %+v", m["a"])
	}
}

func TestDeterministicOrder(t *testing.T) {
	in := []Input{{Model: "z", Results: map[string]int64{}}, {Model: "a", Results: map[string]int64{}}}
	rs, _ := Compute(V1, benches, in)
	if rs[0].Model != "a" {
		t.Fatal("results must be sorted by model")
	}
}

func TestMethodValidation(t *testing.T) {
	bad := []Method{
		{},
		{Version: "x", Weights: []DimensionWeight{{Reasoning, 0}}},
		{Version: "x", Weights: []DimensionWeight{{Other, 1}}},
		{Version: "x", Weights: []DimensionWeight{{Reasoning, 1}, {Reasoning, 2}}},
		{Version: "x", Weights: []DimensionWeight{{Reasoning, 1}}, MinCoverageBps: 10001},
	}
	for i, m := range bad {
		if m.Validate() == nil {
			t.Errorf("method %d accepted", i)
		}
	}
	if V1.Validate() != nil {
		t.Fatal("V1 invalid")
	}
}
