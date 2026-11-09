package llmstats

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/money"
)

// SourceSlug identifies every observation imported from LLM Stats. The
// frontend shows "Data by LLM Stats" whenever the board includes it.
const SourceSlug = "llm-stats"

// Benchmark categories → leaderboard dimensions. Checked in this order, so a
// benchmark tagged both "code" and "reasoning" counts as coding. Categories
// not listed are kept on model pages but do not feed the composite score.
var dimensionCategories = []struct {
	dimension  string
	categories []string
}{
	{"coding", []string{"code", "coding", "programming", "software-engineering", "software_engineering", "swe"}},
	{"agent", []string{"agents", "agent", "agentic", "tool-use", "tool_use", "tools", "function-calling", "function_calling", "computer-use", "computer_use"}},
	{"reasoning", []string{"reasoning", "math", "mathematics", "general", "knowledge", "science", "logic"}},
}

// PriceKey identifies the latest stored price, so an unchanged price is not
// recorded again on every daily sync.
type PriceKey struct{ InputMicro, OutputMicro int64 }

// Report summarises a conversion for logs and the dry-run command.
type Report struct {
	Models, Providers, Offerings, Benchmarks, Prices, Results, Speeds int
	UnchangedPrices, SkippedModels, SkippedScores                     int
	ByDimension                                                       map[string]int
	ByModality                                                        map[string]int
	UnmappedCategories                                                []string
}

func (r Report) String() string {
	return fmt.Sprintf("models=%d providers=%d offerings=%d benchmarks=%d prices=%d (unchanged %d) results=%d speeds=%d skipped_models=%d skipped_scores=%d dimensions=%v modalities=%v unmapped_categories=%v",
		r.Models, r.Providers, r.Offerings, r.Benchmarks, r.Prices, r.UnchangedPrices, r.Results, r.Speeds, r.SkippedModels, r.SkippedScores, r.ByDimension, r.ByModality, r.UnmappedCategories)
}

var (
	nonSlug   = regexp.MustCompile(`[^a-z0-9.]+`)
	sepRuns   = regexp.MustCompile(`[.-]{2,}`)
	trimSeps  = regexp.MustCompile(`^[.-]+|[.-]+$`)
	slugCheck = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
)

// Slug maps an upstream identifier to Intel's slug grammar deterministically.
// Long or empty results get a short hash so distinct inputs stay distinct.
func Slug(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = nonSlug.ReplaceAllString(s, "-")
	s = sepRuns.ReplaceAllString(s, "-")
	s = trimSeps.ReplaceAllString(s, "")
	if s == "" || len(s) > 80 || !slugCheck.MatchString(s) {
		sum := sha256.Sum256([]byte(raw))
		h := hex.EncodeToString(sum[:4])
		if len(s) > 71 {
			s = trimSeps.ReplaceAllString(s[:71], "")
		}
		if s == "" || !slugCheck.MatchString(s) {
			return "x-" + h
		}
		return s + "-" + h
	}
	return s
}

func joinSlug(a, b string) string {
	s := a + "-" + b
	if len(s) <= 80 {
		return s
	}
	return Slug(s)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func httpsURL(candidates ...string) string {
	for _, c := range candidates {
		u, err := url.Parse(c)
		if err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && len(c) <= 500 {
			return c
		}
	}
	return ""
}

func modality(m Model) string {
	t := strings.ToLower(m.ModelType + " " + strings.Join(m.Modalities, " "))
	switch {
	case strings.Contains(t, "embedding"):
		return "embedding"
	case strings.Contains(m.ModelType, "image"):
		return "image"
	case strings.Contains(m.ModelType, "video"):
		return "video"
	case strings.Contains(m.ModelType, "tts") || strings.Contains(m.ModelType, "text-to-speech"):
		return "tts"
	case strings.Contains(m.ModelType, "stt") || strings.Contains(m.ModelType, "speech-to-text") || strings.Contains(m.ModelType, "transcription"):
		return "stt"
	}
	return "llm"
}

func dimensionFor(categories []string) (string, bool) {
	set := map[string]bool{}
	for _, c := range categories {
		set[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, d := range dimensionCategories {
		for _, c := range d.categories {
			if set[c] {
				return d.dimension, true
			}
		}
	}
	return "other", false
}

func scaled(n *json.Number, scale int) (string, int64, bool) {
	if n == nil {
		return "", 0, false
	}
	s, err := money.RoundDecimal(n.String(), scale)
	if err != nil {
		return "", 0, false
	}
	v, err := money.ParseScaled(s, scale)
	return s, v, err == nil
}

// Convert builds an import document. latest holds the newest stored price
// per offering slug; prices equal to it are left out so a daily sync only
// records changes. Speed samples are dated to the sync day, giving at most
// one sample per offering per day.
func Convert(ds Dataset, now time.Time, latest map[string]PriceKey) (catalog.Document, Report) {
	now = now.UTC()
	day := now.Format("2006-01-02")
	rep := Report{ByDimension: map[string]int{}, ByModality: map[string]int{}}
	doc := catalog.Document{
		Source: catalog.Source{Slug: SourceSlug, Note: "LLM Stats API sync " + now.Format(time.RFC3339)},
	}
	providers := map[string]catalog.ProviderDoc{}
	addProvider := func(id, name string) string {
		slug := Slug(id)
		if slug == "x-" || name == "" {
			name = id
		}
		if _, ok := providers[slug]; !ok {
			providers[slug] = catalog.ProviderDoc{Slug: slug, Name: clip(name, 120)}
		}
		return slug
	}

	modelSlugs := map[string]string{}
	seenModels := map[string]bool{}
	for _, m := range ds.Models {
		if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Organization.ID+m.Organization.Name) == "" {
			rep.SkippedModels++
			continue
		}
		slug := Slug(m.ID)
		if seenModels[slug] {
			rep.SkippedModels++
			continue
		}
		seenModels[slug] = true
		modelSlugs[m.ID] = slug
		org := addProvider(firstNonEmpty(m.Organization.ID, m.Organization.Name), m.Organization.Name)
		mod := modality(m)
		rep.ByModality[mod]++
		status := "available"
		release := ""
		if m.ReleaseDate != nil {
			if d, err := time.Parse("2006-01-02", strings.TrimSpace(*m.ReleaseDate)); err == nil {
				release = d.Format("2006-01-02")
				if d.After(now) {
					status = "unreleased"
				}
			}
		}
		license := "proprietary"
		if m.OpenWeight {
			license = clip(firstNonEmpty(m.License.Name, m.License.ID, "open-weights"), 60)
		}
		family := ""
		if m.Family != nil {
			family = clip(m.Family.Name, 60)
		}
		var ctxTokens int64
		if m.ContextWindow != nil && *m.ContextWindow > 0 {
			ctxTokens = *m.ContextWindow
		}
		modelURL := httpsURL(m.URL, "https://llm-stats.com/models/"+url.PathEscape(m.ID))
		doc.Models = append(doc.Models, catalog.ModelDoc{
			Slug: slug, Name: clip(m.Name, 120), Provider: org, Family: family, Modality: mod, ReleaseDate: release,
			Status: status, ContextTokens: ctxTokens, License: license, OpenWeights: m.OpenWeight, SourceURL: modelURL,
		})

		for category, value := range m.TopScores {
			if category != "overall" && category != "reasoning" && category != "code" && category != "agent" {
				continue
			}
			raw, _, ok := scaled(&value, money.MilliScale)
			if !ok {
				continue
			}
			slug := "llmstats-rating-" + category
			yes := true
			doc.Benchmarks = append(doc.Benchmarks, catalog.BenchmarkDoc{Slug: slug, Name: "LLM Stats " + category, Dimension: "other", Unit: "points", HigherIsBetter: &yes, SourceURL: modelURL})
			published := m.UpdatedAt
			if _, err := time.Parse(time.RFC3339, published); err != nil {
				continue
			}
			doc.Results = append(doc.Results, catalog.ResultDoc{Model: Slug(m.ID), Benchmark: slug, Value: raw, PublishedAt: published, SourceURL: modelURL})
		}
		seenOfferings := map[string]bool{}
		for _, p := range m.Providers {
			if strings.TrimSpace(p.ProviderID+p.ProviderName) == "" {
				continue
			}
			host := addProvider(firstNonEmpty(p.ProviderID, p.ProviderName), p.ProviderName)
			off := joinSlug(slug, host)
			if seenOfferings[off] {
				continue
			}
			seenOfferings[off] = true
			doc.Offerings = append(doc.Offerings, catalog.OfferingDoc{Slug: off, Provider: host, Model: slug, Tier: "standard"})
			inText, inMicro, okIn := scaled(p.InputPerM, money.MicroScale)
			outText, outMicro, okOut := scaled(p.OutputPerM, money.MicroScale)
			if okIn && okOut {
				if prev, ok := latest[off]; ok && prev.InputMicro == inMicro && prev.OutputMicro == outMicro {
					rep.UnchangedPrices++
				} else {
					doc.Prices = append(doc.Prices, catalog.PriceDoc{Offering: off, InputUSD: inText, OutputUSD: outText, EffectiveAt: day, SourceURL: modelURL})
				}
			}
			if tps, v, ok := scaled(p.ThroughputTPS, money.MilliScale); ok && v > 0 {
				var ttft int64
				if p.LatencyS != nil {
					if ms, ok := new(big.Rat).SetString(p.LatencyS.String()); ok && ms.Sign() >= 0 {
						ms.Mul(ms, big.NewRat(1000, 1))
						f, _ := ms.Float64()
						ttft = int64(f + 0.5)
					}
				}
				doc.Speeds = append(doc.Speeds, catalog.SpeedDoc{Offering: off, OutputTokPerSec: tps, TTFTMillis: ttft, MeasuredAt: day})
			}
		}
	}

	benchSlugs := map[string]string{}
	benchDocs := map[string]catalog.BenchmarkDoc{}
	unmapped := map[string]bool{}
	yes := true
	for _, b := range ds.Benchmarks {
		if strings.TrimSpace(b.ID) == "" || strings.TrimSpace(b.Name) == "" {
			continue
		}
		dim, ok := dimensionFor(b.Categories)
		if !ok {
			for _, c := range b.Categories {
				unmapped[strings.ToLower(strings.TrimSpace(c))] = true
			}
		}
		slug := Slug("raw-" + b.ID)
		benchSlugs[b.ID] = slug
		benchDocs[slug] = catalog.BenchmarkDoc{
			Slug: slug, Name: clip(b.Name, 120), Dimension: dim, Unit: "points", HigherIsBetter: &yes,
			SourceURL: httpsURL(b.URL, "https://llm-stats.com/benchmarks/"+url.PathEscape(b.ID)),
		}
	}

	usedBench := map[string]bool{}
	seenResults := map[string]bool{}
	for _, s := range ds.Scores {
		model, okM := modelSlugs[s.ModelID]
		bench, okB := benchSlugs[s.BenchmarkID]
		if !okM || !okB {
			rep.SkippedScores++
			continue
		}
		value, _, ok := scaled(&s.Score, money.MilliScale)
		published, err := time.Parse(time.RFC3339, strings.TrimSpace(s.ScoredAt))
		if !ok || err != nil || published.After(now) {
			rep.SkippedScores++
			continue
		}
		settings := ""
		if s.IsSelfReported {
			settings = "self-reported"
		}
		key := model + "\x1f" + bench + "\x1f" + value + "\x1f" + published.UTC().Format(time.RFC3339)
		if seenResults[key] {
			continue
		}
		seenResults[key] = true
		usedBench[bench] = true
		doc.Results = append(doc.Results, catalog.ResultDoc{
			Model: model, Benchmark: bench, Value: value, Settings: settings,
			PublishedAt: published.UTC().Format(time.RFC3339), SourceURL: httpsURL(s.URL, benchDocs[bench].SourceURL),
		})
	}
	for slug := range usedBench {
		doc.Benchmarks = append(doc.Benchmarks, benchDocs[slug])
		rep.ByDimension[benchDocs[slug].Dimension]++
	}
	sort.Slice(doc.Benchmarks, func(i, j int) bool { return doc.Benchmarks[i].Slug < doc.Benchmarks[j].Slug })
	for _, p := range providers {
		doc.Providers = append(doc.Providers, p)
	}
	sort.Slice(doc.Providers, func(i, j int) bool { return doc.Providers[i].Slug < doc.Providers[j].Slug })
	for c := range unmapped {
		if c != "" {
			rep.UnmappedCategories = append(rep.UnmappedCategories, c)
		}
	}
	sort.Strings(rep.UnmappedCategories)
	unique := map[string]bool{}
	defs := doc.Benchmarks[:0]
	for _, b := range doc.Benchmarks {
		if !unique[b.Slug] {
			unique[b.Slug] = true
			defs = append(defs, b)
		}
	}
	doc.Benchmarks = defs
	sort.Slice(doc.Benchmarks, func(i, j int) bool { return doc.Benchmarks[i].Slug < doc.Benchmarks[j].Slug })
	sort.Slice(doc.Results, func(i, j int) bool {
		a, b := doc.Results[i], doc.Results[j]
		return a.Model+"/"+a.Benchmark < b.Model+"/"+b.Benchmark
	})
	rep.Models, rep.Providers, rep.Offerings, rep.Benchmarks = len(doc.Models), len(doc.Providers), len(doc.Offerings), len(doc.Benchmarks)
	rep.Prices, rep.Results, rep.Speeds = len(doc.Prices), len(doc.Results), len(doc.Speeds)
	return doc, rep
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
