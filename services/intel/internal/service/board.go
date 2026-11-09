// Package service turns catalog data and observations into the leaderboard
// snapshot and answers queries against it. Snapshot construction is pure so
// the same inputs always produce the same board and fingerprint.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
)

// Inputs is everything a snapshot is computed from, as loaded from storage.
type Inputs struct {
	Providers  []ProviderInfo
	Models     []ModelInfo
	Benchmarks []score.Benchmark
	Offerings  []OfferingInfo
	// LatestPrice is keyed by offering slug.
	LatestPrice map[string]PriceInfo
	// Results maps model slug → benchmark slug → latest raw value.
	Results map[string]map[string]int64
	// MedianSpeed maps offering slug → median output tokens/s (milli) over the
	// recent observation window.
	MedianSpeed map[string]int64
	// Sources lists every source slug with stored observations; pages must
	// credit them (e.g. "llm-stats" → "Data by LLM Stats").
	Sources []string
}

type ProviderInfo struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type ModelInfo struct {
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`
	Modality      string     `json:"modality"`
	Status        string     `json:"status"`
	License       string     `json:"license"`
	OpenWeights   bool       `json:"open_weights"`
	ReleaseDate   *time.Time `json:"release_date,omitempty"`
	ContextTokens int64      `json:"context_tokens"`
}

type OfferingInfo struct {
	Slug     string `json:"slug"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tier     string `json:"tier"`
}

type PriceInfo struct {
	InputMicro  int64     `json:"input_micro_usd"`
	OutputMicro int64     `json:"output_micro_usd"`
	EffectiveAt time.Time `json:"effective_at"`
}

// Row is one leaderboard line. Pointer fields are nil when data is missing;
// the UI shows "—" and never a fabricated zero.
type Row struct {
	Rank          *int       `json:"rank"`
	Model         string     `json:"model"`
	Name          string     `json:"name"`
	Provider      string     `json:"provider"`
	ProviderName  string     `json:"provider_name"`
	Status        string     `json:"status"`
	License       string     `json:"license"`
	OpenWeights   bool       `json:"open_weights"`
	ReleaseDate   *time.Time `json:"release_date,omitempty"`
	ContextTokens *int64     `json:"context_tokens"`
	Composite     *int64     `json:"composite_bps"`
	Reasoning     *int64     `json:"reasoning_bps"`
	Coding        *int64     `json:"coding_bps"`
	Agent         *int64     `json:"agent_bps"`
	CoverageBps   int64      `json:"coverage_bps"`
	InputPrice    *int64     `json:"input_micro_usd"`
	OutputPrice   *int64     `json:"output_micro_usd"`
	BlendedPrice  *int64     `json:"blended_micro_usd"`
	PriceOffering string     `json:"price_offering,omitempty"`
	SpeedMilli    *int64     `json:"output_tokens_per_sec_milli"`
}

type Snapshot struct {
	MethodVersion string    `json:"method_version"`
	Fingerprint   string    `json:"fingerprint"`
	ComputedAt    time.Time `json:"computed_at"`
	Sources       []string  `json:"sources"`
	Rows          []Row     `json:"rows"`
}

// BuildSnapshot scores LLMs and materialises every column the board shows.
// Only the llm modality is ranked in M1; other modalities wait for their own
// methods.
func BuildSnapshot(method score.Method, in Inputs, now time.Time) (Snapshot, error) {
	providerNames := map[string]string{}
	for _, p := range in.Providers {
		providerNames[p.Slug] = p.Name
	}
	models := make([]ModelInfo, 0, len(in.Models))
	for _, m := range in.Models {
		if m.Modality == "llm" && m.Status != "deprecated" {
			models = append(models, m)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Slug < models[j].Slug })

	scoreInputs := make([]score.Input, 0, len(models))
	for _, m := range models {
		scoreInputs = append(scoreInputs, score.Input{Model: m.Slug, Results: in.Results[m.Slug]})
	}
	var scored []score.Result
	var err error
	if method.Version == score.Source.Version {
		scored = nil
		for _, m := range models {
			r := score.Result{Model: m.Slug, Dimensions: map[score.Dimension]int64{}}
			raw := in.Results[m.Slug]
			if v, ok := raw["llmstats-rating-overall"]; ok {
				v = v / 10
				r.Composite = &v
			}
			for d, k := range map[score.Dimension]string{score.Reasoning: "reasoning", score.Coding: "code", score.Agent: "agent"} {
				if v, ok := raw["llmstats-rating-"+k]; ok {
					r.Dimensions[d] = v / 10
				}
			}
			scored = append(scored, r)
		}
	}
	if method.Version != score.Source.Version {
		scored, err = score.Compute(method, in.Benchmarks, scoreInputs)
	}
	if err != nil {
		return Snapshot{}, err
	}
	byModel := map[string]score.Result{}
	for _, s := range scored {
		byModel[s.Model] = s
	}

	offeringsByModel := map[string][]OfferingInfo{}
	for _, o := range in.Offerings {
		offeringsByModel[o.Model] = append(offeringsByModel[o.Model], o)
	}

	rows := make([]Row, 0, len(models))
	for _, m := range models {
		s := byModel[m.Slug]
		row := Row{
			Model: m.Slug, Name: m.Name, Provider: m.Provider, ProviderName: providerNames[m.Provider],
			Status: m.Status, License: m.License, OpenWeights: m.OpenWeights, ReleaseDate: m.ReleaseDate,
			Composite: s.Composite, CoverageBps: s.CoverageBps,
			Reasoning: dim(s, score.Reasoning), Coding: dim(s, score.Coding), Agent: dim(s, score.Agent),
		}
		if m.ContextTokens > 0 {
			v := m.ContextTokens
			row.ContextTokens = &v
		}
		if o, p, ok := referencePrice(m, offeringsByModel[m.Slug], in.LatestPrice); ok {
			blended, err := money.BlendedPrice(p.InputMicro, p.OutputMicro)
			if err != nil {
				return Snapshot{}, err
			}
			inputPrice, outputPrice := p.InputMicro, p.OutputMicro
			row.InputPrice, row.OutputPrice, row.BlendedPrice, row.PriceOffering = &inputPrice, &outputPrice, &blended, o.Slug
		}
		if v, ok := referenceSpeed(m, offeringsByModel[m.Slug], in.MedianSpeed); ok {
			row.SpeedMilli = &v
		}
		rows = append(rows, row)
	}
	rankRows(rows)

	sources := append([]string{}, in.Sources...)
	sort.Strings(sources)
	snap := Snapshot{MethodVersion: method.Version, ComputedAt: now.UTC(), Sources: sources, Rows: rows}
	// The fingerprint covers what is shown, not when it was computed, so an
	// unchanged board is recognised and not stored twice.
	payload, err := json.Marshal(struct {
		M string
		S []string
		R []Row
	}{method.Version, sources, rows})
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(payload)
	snap.Fingerprint = hex.EncodeToString(sum[:])
	return snap, nil
}

func dim(s score.Result, d score.Dimension) *int64 {
	if v, ok := s.Dimensions[d]; ok {
		return &v
	}
	return nil
}

// referencePrice prefers the model maker's own standard offering, which is
// what a reader expects "the price" to mean; otherwise it takes the cheapest
// priced offering so a hosted-only open model still shows a number.
func referencePrice(m ModelInfo, offerings []OfferingInfo, prices map[string]PriceInfo) (OfferingInfo, PriceInfo, bool) {
	var best OfferingInfo
	var bestPrice PriceInfo
	var bestBlend int64
	found := false
	for _, o := range sortedOfferings(offerings) {
		p, ok := prices[o.Slug]
		if !ok {
			continue
		}
		if o.Provider == m.Provider && o.Tier == "standard" {
			return o, p, true
		}
		blend, err := money.BlendedPrice(p.InputMicro, p.OutputMicro)
		if err != nil {
			continue
		}
		if !found || blend < bestBlend {
			best, bestPrice, bestBlend, found = o, p, blend, true
		}
	}
	return best, bestPrice, found
}

func referenceSpeed(m ModelInfo, offerings []OfferingInfo, speeds map[string]int64) (int64, bool) {
	var best int64
	found := false
	for _, o := range sortedOfferings(offerings) {
		v, ok := speeds[o.Slug]
		if !ok {
			continue
		}
		if o.Provider == m.Provider && o.Tier == "standard" {
			return v, true
		}
		if !found || v > best {
			best, found = v, true
		}
	}
	return best, found
}

func sortedOfferings(in []OfferingInfo) []OfferingInfo {
	out := append([]OfferingInfo(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// rankRows orders by composite (scored first), then name, and assigns
// competition ranks (1, 2, 2, 4) to scored rows only.
func rankRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Composite, rows[j].Composite
		if (a == nil) != (b == nil) {
			return a != nil
		}
		if a != nil && *a != *b {
			return *a > *b
		}
		return rows[i].Model < rows[j].Model
	})
	for i := range rows {
		if rows[i].Composite == nil {
			break
		}
		r := i + 1
		if i > 0 && *rows[i-1].Composite == *rows[i].Composite {
			r = *rows[i-1].Rank
		}
		rows[i].Rank = &r
	}
}

var ErrNoSnapshot = errors.New("no leaderboard snapshot yet")
