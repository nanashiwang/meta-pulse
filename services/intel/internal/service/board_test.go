package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
)

var now = time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)

func day(daysAgo int) *time.Time { d := now.AddDate(0, 0, -daysAgo); return &d }

func fixture() Inputs {
	return Inputs{
		Providers: []ProviderInfo{{"lab-a", "Lab A"}, {"lab-b", "Lab B"}, {"host", "Host"}},
		Models: []ModelInfo{
			{Slug: "alpha", Name: "Alpha", Provider: "lab-a", Modality: "llm", Status: "available", ReleaseDate: day(5), ContextTokens: 1_000_000},
			{Slug: "beta", Name: "Beta", Provider: "lab-b", Modality: "llm", Status: "available", OpenWeights: true, License: "apache-2.0", ReleaseDate: day(60), ContextTokens: 128_000},
			{Slug: "gamma", Name: "Gamma", Provider: "lab-a", Modality: "llm", Status: "unreleased"},
			{Slug: "old", Name: "Old", Provider: "lab-a", Modality: "llm", Status: "deprecated"},
			{Slug: "pic", Name: "Pic", Provider: "lab-a", Modality: "image", Status: "available"},
		},
		Benchmarks: []score.Benchmark{
			{Slug: "r", Dimension: score.Reasoning, HigherIsBetter: true},
			{Slug: "c", Dimension: score.Coding, HigherIsBetter: true},
			{Slug: "a", Dimension: score.Agent, HigherIsBetter: true},
		},
		Offerings: []OfferingInfo{
			{Slug: "lab-a-alpha", Provider: "lab-a", Model: "alpha", Tier: "standard"},
			{Slug: "host-alpha", Provider: "host", Model: "alpha", Tier: "standard"},
			{Slug: "host-beta", Provider: "host", Model: "beta", Tier: "standard"},
			{Slug: "host-beta-2", Provider: "host", Model: "beta", Tier: "batch"},
		},
		LatestPrice: map[string]PriceInfo{
			"lab-a-alpha": {InputMicro: 5_000_000, OutputMicro: 15_000_000},
			"host-alpha":  {InputMicro: 1_000_000, OutputMicro: 1_000_000},
			"host-beta":   {InputMicro: 400_000, OutputMicro: 800_000},
			"host-beta-2": {InputMicro: 200_000, OutputMicro: 400_000},
		},
		Results: map[string]map[string]int64{
			"alpha": {"r": 9000, "c": 8000, "a": 7000},
			"beta":  {"r": 5000, "c": 9000, "a": 5000},
			"gamma": {"r": 1000},
		},
		MedianSpeed: map[string]int64{"host-alpha": 90_000, "host-beta": 210_000},
	}
}

func build(t *testing.T) Snapshot {
	t.Helper()
	s, err := BuildSnapshot(score.V1, fixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func row(s Snapshot, slug string) Row {
	for _, r := range s.Rows {
		if r.Model == slug {
			return r
		}
	}
	return Row{}
}

func TestSnapshotRanksAndExcludes(t *testing.T) {
	s := build(t)
	if len(s.Rows) != 3 {
		t.Fatalf("deprecated and non-llm rows must be excluded, got %d", len(s.Rows))
	}
	if s.Rows[0].Model != "alpha" || *s.Rows[0].Rank != 1 || *s.Rows[1].Rank != 2 {
		t.Fatalf("ranking wrong: %+v", s.Rows)
	}
	if g := row(s, "gamma"); g.Rank != nil || g.Composite != nil || g.Model != "gamma" {
		t.Fatalf("insufficient model must be unranked and last: %+v", g)
	}
}

func TestEqualCompositesShareRank(t *testing.T) {
	rows := []Row{{Model: "a", Composite: ptr(10)}, {Model: "b", Composite: ptr(10)}, {Model: "c", Composite: ptr(5)}, {Model: "d"}}
	rankRows(rows)
	if *rows[0].Rank != 1 || *rows[1].Rank != 1 || *rows[2].Rank != 3 || rows[3].Rank != nil {
		t.Fatalf("competition ranking wrong")
	}
}

func ptr(v int64) *int64 { return &v }

func TestReferencePriceAndSpeed(t *testing.T) {
	s := build(t)
	a := row(s, "alpha")
	if a.PriceOffering != "lab-a-alpha" || *a.BlendedPrice != 7_500_000 {
		t.Fatalf("maker's own standard offering must win: %s %d", a.PriceOffering, *a.BlendedPrice)
	}
	if *a.SpeedMilli != 90_000 {
		t.Fatalf("speed falls back to hosted offering: %d", *a.SpeedMilli)
	}
	b := row(s, "beta")
	if b.PriceOffering != "host-beta-2" {
		t.Fatalf("without a maker offering, cheapest wins: %s", b.PriceOffering)
	}
	if g := row(s, "gamma"); g.BlendedPrice != nil || g.SpeedMilli != nil || g.ContextTokens != nil {
		t.Fatal("missing data must stay nil")
	}
}

func TestFingerprintStableAcrossTime(t *testing.T) {
	a, _ := BuildSnapshot(score.V1, fixture(), now)
	b, _ := BuildSnapshot(score.V1, fixture(), now.Add(time.Hour))
	if a.Fingerprint != b.Fingerprint {
		t.Fatal("same inputs must fingerprint the same regardless of compute time")
	}
	in := fixture()
	in.LatestPrice["lab-a-alpha"] = PriceInfo{InputMicro: 1, OutputMicro: 1}
	c, _ := BuildSnapshot(score.V1, in, now)
	if c.Fingerprint == a.Fingerprint {
		t.Fatal("changed price must change the fingerprint")
	}
}

func TestFilterSortAndPage(t *testing.T) {
	s := build(t)
	q := Query{Sort: "price"}
	if err := q.Normalize(); err != nil {
		t.Fatal(err)
	}
	p := Filter(s, q, now)
	if p.Rows[0].Model != "beta" || p.Rows[2].Model != "gamma" {
		t.Fatalf("cheapest first, missing last: %v", models(p.Rows))
	}
	q = Query{Window: "30d"}
	_ = q.Normalize()
	if p := Filter(s, q, now); p.Total != 1 || p.Rows[0].Model != "alpha" {
		t.Fatalf("30d window: %v", models(p.Rows))
	}
	q = Query{OpenOnly: true}
	_ = q.Normalize()
	if p := Filter(s, q, now); p.Total != 1 || *p.Rows[0].Rank != 2 {
		t.Fatal("filtered view must keep the global rank")
	}
	q = Query{Page: 5}
	_ = q.Normalize()
	if p := Filter(s, q, now); len(p.Rows) != 0 || p.Total != 3 {
		t.Fatal("page past the end is empty, not an error")
	}
	for _, bad := range []Query{{Sort: "name; drop"}, {Window: "7d"}, {Page: -1}, {Page: MaxPage + 1}} {
		if bad.Normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestLeadersAndRecent(t *testing.T) {
	s := build(t)
	got := map[string]string{}
	for _, l := range Leaders(s) {
		got[l.Category] = l.Row.Model
	}
	want := map[string]string{"reasoning": "alpha", "coding": "beta", "value": "beta", "speed": "beta", "context": "alpha", "open": "beta"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("leader %s = %q, want %q", k, got[k], v)
		}
	}
	r := Recent(s, now, 5)
	if len(r) != 1 || r[0].Model != "alpha" {
		t.Fatalf("recent: %v", models(r))
	}
}

func models(rows []Row) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Model)
	}
	return out
}

func TestEmptyBoardEncodesArraysAndIsNotReady(t *testing.T) {
	empty, err := BuildSnapshot(score.V1, Inputs{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if Leaders(empty) == nil || Recent(empty, now, 5) == nil {
		t.Fatal("leaders/recent must encode as [] not null")
	}
	r := NewReader(stubStore{snap: empty}, score.V1, time.Minute)
	if _, err := r.Home(context.Background()); err != ErrNoSnapshot {
		t.Fatalf("empty board must read as not ready, got %v", err)
	}
}

type stubStore struct{ snap Snapshot }

func (s stubStore) LatestSnapshot(context.Context, string) (Snapshot, error) { return s.snap, nil }
func (s stubStore) ModelDetail(context.Context, string, time.Time) (ModelDetail, error) {
	return ModelDetail{}, ErrNotFound
}
func (s stubStore) Benchmarks(context.Context) ([]BenchmarkInfo, error) { return nil, nil }

func TestSourcesAreSortedAndFingerprinted(t *testing.T) {
	in := fixture()
	in.Sources = []string{"manual", "llm-stats"}
	a, _ := BuildSnapshot(score.V1, in, now)
	if strings.Join(a.Sources, ",") != "llm-stats,manual" {
		t.Fatalf("sources %v", a.Sources)
	}
	b, _ := BuildSnapshot(score.V1, fixture(), now)
	if a.Fingerprint == b.Fingerprint {
		t.Fatal("adding a source must change the board fingerprint so attribution updates")
	}
}

func TestSourceScoresRemainOriginalAndMissingStayMissing(t *testing.T) {
	in := Inputs{Models: []ModelInfo{{Slug: "opus", Modality: "llm"}, {Slug: "qwen", Modality: "llm"}}, Results: map[string]map[string]int64{"opus": {"llmstats-rating-overall": 59900, "llmstats-rating-code": 61250}, "qwen": {"raw-bench": 100000}}}
	snap, err := BuildSnapshot(score.Source, in, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if *snap.Rows[0].Composite != 5990 || *snap.Rows[0].Coding != 6125 {
		t.Fatalf("source scores changed: %+v", snap.Rows[0])
	}
	if snap.Rows[1].Composite != nil || snap.Rows[1].Reasoning != nil {
		t.Fatal("missing source score was synthesized")
	}
	in.Models = append(in.Models, ModelInfo{Slug: "extra", Modality: "llm"})
	again, _ := BuildSnapshot(score.Source, in, time.Now())
	if *again.Rows[0].Composite != 5990 {
		t.Fatal("other models changed source score")
	}
}
