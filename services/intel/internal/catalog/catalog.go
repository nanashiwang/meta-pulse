// Package catalog defines the import document and its validated, integer
// form. Catalog rows (providers, models, benchmarks, offerings) are upserted
// by slug; observations (prices, results, speeds) are append-only and carry a
// content fingerprint so replaying the same document writes nothing new.
package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/money"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

const (
	maxName = 120
	maxNote = 500
)

// Document is the JSON accepted by `intel-tool import`.
type Document struct {
	Source     Source         `json:"source"`
	Providers  []ProviderDoc  `json:"providers"`
	Models     []ModelDoc     `json:"models"`
	Benchmarks []BenchmarkDoc `json:"benchmarks"`
	Offerings  []OfferingDoc  `json:"offerings"`
	Prices     []PriceDoc     `json:"prices"`
	Results    []ResultDoc    `json:"benchmark_results"`
	Speeds     []SpeedDoc     `json:"speed_samples"`
}

// Source names who supplied the document. Every observation records it.
type Source struct {
	Slug string `json:"slug"`
	Note string `json:"note"`
}

type ProviderDoc struct {
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Website string `json:"website"`
}

type ModelDoc struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Family          string `json:"family"`
	Modality        string `json:"modality"`
	ReleaseDate     string `json:"release_date"`
	Status          string `json:"status"`
	ContextTokens   int64  `json:"context_tokens"`
	MaxOutputTokens int64  `json:"max_output_tokens"`
	License         string `json:"license"`
	OpenWeights     bool   `json:"open_weights"`
	SourceURL       string `json:"source_url"`
}

type BenchmarkDoc struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Dimension      string `json:"dimension"`
	Unit           string `json:"unit"`
	HigherIsBetter *bool  `json:"higher_is_better"`
	SourceURL      string `json:"source_url"`
}

type OfferingDoc struct {
	Slug     string `json:"slug"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tier     string `json:"tier"`
}

type PriceDoc struct {
	Offering     string `json:"offering"`
	InputUSD     string `json:"input_usd_per_mtok"`
	OutputUSD    string `json:"output_usd_per_mtok"`
	CacheReadUSD string `json:"cache_read_usd_per_mtok"`
	EffectiveAt  string `json:"effective_at"`
	SourceURL    string `json:"source_url"`
}

type ResultDoc struct {
	Model       string `json:"model"`
	Benchmark   string `json:"benchmark"`
	Value       string `json:"value"`
	Settings    string `json:"settings"`
	SelfRun     bool   `json:"self_run"`
	PublishedAt string `json:"published_at"`
	SourceURL   string `json:"source_url"`
}

type SpeedDoc struct {
	Offering        string `json:"offering"`
	OutputTokPerSec string `json:"output_tokens_per_sec"`
	TTFTMillis      int64  `json:"ttft_ms"`
	MeasuredAt      string `json:"measured_at"`
}

// Validated, integer forms.

type Provider struct{ Slug, Name, Website string }

type Model struct {
	Slug, Name, Provider, Family, Modality, Status, License, SourceURL string
	ReleaseDate                                                        *time.Time
	ContextTokens, MaxOutputTokens                                     int64
	OpenWeights                                                        bool
}

type Benchmark struct {
	Slug, Name, Unit, SourceURL string
	Dimension                   score.Dimension
	HigherIsBetter              bool
}

type Offering struct{ Slug, Provider, Model, Tier string }

type Price struct {
	Offering                string
	InputMicro, OutputMicro int64
	CacheReadMicro          *int64
	EffectiveAt             time.Time
	SourceURL, Fingerprint  string
}

type Result struct {
	Model, Benchmark, Settings, SourceURL, Fingerprint string
	Value                                              int64
	SelfRun                                            bool
	PublishedAt                                        time.Time
}

type Speed struct {
	Offering, Fingerprint string
	OutputTokPerSecMilli  int64
	TTFTMillis            int64
	MeasuredAt            time.Time
}

type Batch struct {
	Source     Source
	Providers  []Provider
	Models     []Model
	Benchmarks []Benchmark
	Offerings  []Offering
	Prices     []Price
	Results    []Result
	Speeds     []Speed
}

// Known looks up catalog slugs that already exist in storage, so a document
// may reference rows imported earlier.
type Known struct {
	Providers, Models, Benchmarks, Offerings map[string]bool
	// BenchmarkUnits maps an existing benchmark slug to its unit.
	BenchmarkUnits map[string]string
}

var (
	modalities = map[string]bool{"llm": true, "image": true, "video": true, "tts": true, "stt": true, "embedding": true}
	statuses   = map[string]bool{"available": true, "unreleased": true, "deprecated": true}
	dimensions = map[string]score.Dimension{"reasoning": score.Reasoning, "coding": score.Coding, "agent": score.Agent, "other": score.Other}
	units      = map[string]int{"percent": money.BpsScale, "points": money.MilliScale}
)

type errs []string

func (e *errs) add(format string, args ...any) { *e = append(*e, fmt.Sprintf(format, args...)) }

// Validate converts a document into a Batch, reporting every problem at once
// so an operator can fix a whole file in one pass.
func Validate(doc Document, known Known) (Batch, error) {
	var problems errs
	b := Batch{Source: doc.Source}
	if !slugPattern.MatchString(doc.Source.Slug) {
		problems.add("source.slug %q must be a lowercase slug", doc.Source.Slug)
	}
	if len(doc.Source.Note) > maxNote {
		problems.add("source.note too long")
	}
	has := func(set map[string]bool, local map[string]bool, slug string) bool { return set[slug] || local[slug] }
	providers, models, benchmarks, offerings := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	benchUnits := map[string]string{}
	for k, v := range known.BenchmarkUnits {
		benchUnits[k] = v
	}

	for i, p := range doc.Providers {
		at := fmt.Sprintf("providers[%d]", i)
		checkSlug(&problems, at, p.Slug, providers)
		checkName(&problems, at, p.Name)
		checkURL(&problems, at+".website", p.Website, false)
		b.Providers = append(b.Providers, Provider{p.Slug, strings.TrimSpace(p.Name), p.Website})
	}
	for i, m := range doc.Models {
		at := fmt.Sprintf("models[%d]", i)
		checkSlug(&problems, at, m.Slug, models)
		checkName(&problems, at, m.Name)
		if !has(known.Providers, providers, m.Provider) {
			problems.add("%s.provider %q is unknown", at, m.Provider)
		}
		modality := defaultString(m.Modality, "llm")
		if !modalities[modality] {
			problems.add("%s.modality %q is not supported", at, m.Modality)
		}
		status := defaultString(m.Status, "available")
		if !statuses[status] {
			problems.add("%s.status %q is not supported", at, m.Status)
		}
		if m.ContextTokens < 0 || m.MaxOutputTokens < 0 {
			problems.add("%s token limits must not be negative", at)
		}
		if len(m.License) > 60 || len(m.Family) > 60 {
			problems.add("%s license/family too long", at)
		}
		checkURL(&problems, at+".source_url", m.SourceURL, false)
		var release *time.Time
		if m.ReleaseDate != "" {
			d, err := time.Parse("2006-01-02", m.ReleaseDate)
			if err != nil {
				problems.add("%s.release_date must be YYYY-MM-DD", at)
			} else {
				release = &d
			}
		}
		b.Models = append(b.Models, Model{
			Slug: m.Slug, Name: strings.TrimSpace(m.Name), Provider: m.Provider, Family: m.Family,
			Modality: modality, Status: status, License: defaultString(m.License, "proprietary"), SourceURL: m.SourceURL,
			ReleaseDate: release, ContextTokens: m.ContextTokens, MaxOutputTokens: m.MaxOutputTokens, OpenWeights: m.OpenWeights,
		})
	}
	for i, x := range doc.Benchmarks {
		at := fmt.Sprintf("benchmarks[%d]", i)
		checkSlug(&problems, at, x.Slug, benchmarks)
		checkName(&problems, at, x.Name)
		dim, ok := dimensions[x.Dimension]
		if !ok {
			problems.add("%s.dimension %q is not supported", at, x.Dimension)
		}
		if _, ok := units[x.Unit]; !ok {
			problems.add("%s.unit must be percent or points", at)
		}
		if prev, ok := known.BenchmarkUnits[x.Slug]; ok && prev != x.Unit {
			problems.add("%s.unit cannot change from %q once results exist", at, prev)
		}
		benchUnits[x.Slug] = x.Unit
		if x.HigherIsBetter == nil {
			problems.add("%s.higher_is_better is required", at)
		}
		checkURL(&problems, at+".source_url", x.SourceURL, true)
		hib := x.HigherIsBetter != nil && *x.HigherIsBetter
		b.Benchmarks = append(b.Benchmarks, Benchmark{Slug: x.Slug, Name: strings.TrimSpace(x.Name), Unit: x.Unit, SourceURL: x.SourceURL, Dimension: dim, HigherIsBetter: hib})
	}
	for i, o := range doc.Offerings {
		at := fmt.Sprintf("offerings[%d]", i)
		checkSlug(&problems, at, o.Slug, offerings)
		if !has(known.Providers, providers, o.Provider) {
			problems.add("%s.provider %q is unknown", at, o.Provider)
		}
		if !has(known.Models, models, o.Model) {
			problems.add("%s.model %q is unknown", at, o.Model)
		}
		tier := defaultString(o.Tier, "standard")
		if !slugPattern.MatchString(tier) {
			problems.add("%s.tier must be a slug", at)
		}
		b.Offerings = append(b.Offerings, Offering{o.Slug, o.Provider, o.Model, tier})
	}
	for i, p := range doc.Prices {
		at := fmt.Sprintf("prices[%d]", i)
		if !has(known.Offerings, offerings, p.Offering) {
			problems.add("%s.offering %q is unknown", at, p.Offering)
		}
		in, err1 := money.ParseScaled(p.InputUSD, money.MicroScale)
		out, err2 := money.ParseScaled(p.OutputUSD, money.MicroScale)
		if err1 != nil || err2 != nil {
			problems.add("%s input/output price must be a plain decimal USD amount", at)
		}
		var cache *int64
		if p.CacheReadUSD != "" {
			v, err := money.ParseScaled(p.CacheReadUSD, money.MicroScale)
			if err != nil {
				problems.add("%s.cache_read_usd_per_mtok invalid", at)
			}
			cache = &v
		}
		eff := parseTime(&problems, at+".effective_at", p.EffectiveAt)
		checkURL(&problems, at+".source_url", p.SourceURL, true)
		pr := Price{Offering: p.Offering, InputMicro: in, OutputMicro: out, CacheReadMicro: cache, EffectiveAt: eff, SourceURL: p.SourceURL}
		cacheText := ""
		if cache != nil {
			cacheText = fmt.Sprint(*cache)
		}
		pr.Fingerprint = fingerprint("price", pr.Offering, fmt.Sprint(in), fmt.Sprint(out), cacheText, eff.UTC().Format(time.RFC3339))
		b.Prices = append(b.Prices, pr)
	}
	for i, r := range doc.Results {
		at := fmt.Sprintf("benchmark_results[%d]", i)
		if !has(known.Models, models, r.Model) {
			problems.add("%s.model %q is unknown", at, r.Model)
		}
		unit, ok := benchUnits[r.Benchmark]
		if !ok && !has(known.Benchmarks, benchmarks, r.Benchmark) {
			problems.add("%s.benchmark %q is unknown", at, r.Benchmark)
		}
		v, err := money.ParseScaled(r.Value, units[unit])
		if ok && err != nil {
			problems.add("%s.value must be a plain decimal (%s, at most %d decimals)", at, unit, units[unit])
		}
		if unit == "percent" && v > 10000 {
			problems.add("%s.value exceeds 100%%", at)
		}
		if len(r.Settings) > 120 {
			problems.add("%s.settings too long", at)
		}
		pub := parseTime(&problems, at+".published_at", r.PublishedAt)
		checkURL(&problems, at+".source_url", r.SourceURL, !r.SelfRun)
		res := Result{Model: r.Model, Benchmark: r.Benchmark, Settings: r.Settings, SourceURL: r.SourceURL, Value: v, SelfRun: r.SelfRun, PublishedAt: pub}
		res.Fingerprint = fingerprint("result", res.Model, res.Benchmark, fmt.Sprint(v), res.Settings, pub.UTC().Format(time.RFC3339))
		b.Results = append(b.Results, res)
	}
	for i, s := range doc.Speeds {
		at := fmt.Sprintf("speed_samples[%d]", i)
		if !has(known.Offerings, offerings, s.Offering) {
			problems.add("%s.offering %q is unknown", at, s.Offering)
		}
		v, err := money.ParseScaled(s.OutputTokPerSec, money.MilliScale)
		if err != nil || v == 0 {
			problems.add("%s.output_tokens_per_sec must be a positive decimal", at)
		}
		if s.TTFTMillis < 0 {
			problems.add("%s.ttft_ms must not be negative", at)
		}
		m := parseTime(&problems, at+".measured_at", s.MeasuredAt)
		sp := Speed{Offering: s.Offering, OutputTokPerSecMilli: v, TTFTMillis: s.TTFTMillis, MeasuredAt: m}
		sp.Fingerprint = fingerprint("speed", sp.Offering, fmt.Sprint(v), fmt.Sprint(s.TTFTMillis), m.UTC().Format(time.RFC3339))
		b.Speeds = append(b.Speeds, sp)
	}
	if len(problems) > 0 {
		return Batch{}, &ValidationError{Problems: problems}
	}
	return b, nil
}

// ValidationError lists every problem found in a document.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "invalid import document:\n  " + strings.Join(e.Problems, "\n  ")
}

// MaxDocumentBytes bounds a single import document.
const MaxDocumentBytes = 16 << 20

// DecodeDocument parses exactly one JSON object and rejects unknown fields,
// so a typo cannot silently drop data.
func DecodeDocument(raw []byte) (Document, error) {
	var doc Document
	if len(raw) > MaxDocumentBytes {
		return doc, errors.New("import document exceeds 16 MiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return doc, fmt.Errorf("decode import document: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return doc, errors.New("import document must contain a single JSON object")
	}
	return doc, nil
}

// Summary counts the rows in a validated batch.
func (b Batch) Summary() map[string]int {
	return map[string]int{"providers": len(b.Providers), "models": len(b.Models), "benchmarks": len(b.Benchmarks), "offerings": len(b.Offerings),
		"prices": len(b.Prices), "benchmark_results": len(b.Results), "speed_samples": len(b.Speeds)}
}

func checkSlug(p *errs, at, slug string, seen map[string]bool) {
	if !slugPattern.MatchString(slug) || len(slug) > 80 {
		p.add("%s.slug %q must be a lowercase slug", at, slug)
	}
	if seen[slug] {
		p.add("%s.slug %q is duplicated", at, slug)
	}
	seen[slug] = true
}

func checkName(p *errs, at, name string) {
	if n := strings.TrimSpace(name); n == "" || len(n) > maxName {
		p.add("%s.name must be 1-%d characters", at, maxName)
	}
}

// checkURL accepts only public https URLs without credentials or fragments;
// observations from a third party must cite where the number came from.
func checkURL(p *errs, at, raw string, required bool) {
	if raw == "" {
		if required {
			p.add("%s is required", at)
		}
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(raw) > 500 {
		p.add("%s must be a public https URL", at)
	}
}

func parseTime(p *errs, at, raw string) time.Time {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if d, err := time.Parse("2006-01-02", raw); err == nil {
		return d
	}
	p.add("%s must be RFC3339 or YYYY-MM-DD", at)
	return time.Time{}
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func fingerprint(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:])
}
