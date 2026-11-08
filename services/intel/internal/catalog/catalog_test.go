package catalog

import (
	"strings"
	"testing"
)

func yes() *bool { v := true; return &v }

func validDoc() Document {
	return Document{
		Source:     Source{Slug: "manual-test"},
		Providers:  []ProviderDoc{{Slug: "lab-a", Name: "Lab A", Website: "https://lab-a.example"}},
		Models:     []ModelDoc{{Slug: "model-one", Name: "Model One", Provider: "lab-a", ReleaseDate: "2026-10-01", ContextTokens: 200000}},
		Benchmarks: []BenchmarkDoc{{Slug: "bench-r", Name: "Bench R", Dimension: "reasoning", Unit: "percent", HigherIsBetter: yes(), SourceURL: "https://bench.example/r"}},
		Offerings:  []OfferingDoc{{Slug: "lab-a-model-one", Provider: "lab-a", Model: "model-one"}},
		Prices:     []PriceDoc{{Offering: "lab-a-model-one", InputUSD: "2.50", OutputUSD: "10", EffectiveAt: "2026-10-01", SourceURL: "https://lab-a.example/pricing"}},
		Results:    []ResultDoc{{Model: "model-one", Benchmark: "bench-r", Value: "87.3", PublishedAt: "2026-10-02", SourceURL: "https://bench.example/r"}},
		Speeds:     []SpeedDoc{{Offering: "lab-a-model-one", OutputTokPerSec: "123.4", TTFTMillis: 450, MeasuredAt: "2026-10-03T10:00:00Z"}},
	}
}

func TestValidateConvertsToIntegers(t *testing.T) {
	b, err := Validate(validDoc(), Known{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Prices[0].InputMicro != 2_500_000 || b.Prices[0].OutputMicro != 10_000_000 {
		t.Fatalf("price %+v", b.Prices[0])
	}
	if b.Results[0].Value != 8730 || b.Speeds[0].OutputTokPerSecMilli != 123_400 {
		t.Fatalf("result/speed %+v %+v", b.Results[0], b.Speeds[0])
	}
	if b.Models[0].Modality != "llm" || b.Models[0].Status != "available" || b.Models[0].License != "proprietary" {
		t.Fatalf("defaults %+v", b.Models[0])
	}
	again, _ := Validate(validDoc(), Known{})
	if again.Prices[0].Fingerprint != b.Prices[0].Fingerprint || again.Results[0].Fingerprint != b.Results[0].Fingerprint {
		t.Fatal("fingerprints must be stable for replay")
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	d := validDoc()
	d.Models[0].Provider = "missing"
	d.Prices[0].InputUSD = "2.5e1"
	d.Prices[0].SourceURL = "http://insecure.example"
	d.Results[0].Value = "101"
	d.Benchmarks[0].HigherIsBetter = nil
	_, err := Validate(d, Known{})
	if err == nil {
		t.Fatal("invalid document accepted")
	}
	for _, want := range []string{"provider \"missing\"", "plain decimal USD", "public https", "exceeds 100%", "higher_is_better"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestValidateUsesKnownRowsAndLocksUnits(t *testing.T) {
	d := Document{
		Source:  Source{Slug: "later"},
		Results: []ResultDoc{{Model: "model-one", Benchmark: "bench-r", Value: "50", PublishedAt: "2026-11-01", SourceURL: "https://bench.example/r"}},
	}
	known := Known{Models: map[string]bool{"model-one": true}, Benchmarks: map[string]bool{"bench-r": true}, BenchmarkUnits: map[string]string{"bench-r": "percent"}}
	if _, err := Validate(d, known); err != nil {
		t.Fatal(err)
	}
	d.Benchmarks = []BenchmarkDoc{{Slug: "bench-r", Name: "Bench R", Dimension: "reasoning", Unit: "points", HigherIsBetter: yes(), SourceURL: "https://bench.example/r"}}
	if _, err := Validate(d, known); err == nil || !strings.Contains(err.Error(), "cannot change") {
		t.Fatalf("unit change accepted: %v", err)
	}
}

func TestThirdPartyResultsNeedSource(t *testing.T) {
	d := validDoc()
	d.Results[0].SourceURL = ""
	if _, err := Validate(d, Known{}); err == nil {
		t.Fatal("unsourced third-party result accepted")
	}
	d.Results[0].SelfRun = true
	if _, err := Validate(d, Known{}); err != nil {
		t.Fatalf("self-run result without URL rejected: %v", err)
	}
}
