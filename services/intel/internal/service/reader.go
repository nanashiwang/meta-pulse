package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
)

var ErrNotFound = errors.New("not found")

type ModelDetail struct {
	Slug            string            `json:"slug"`
	Name            string            `json:"name"`
	Provider        string            `json:"provider"`
	ProviderName    string            `json:"provider_name"`
	ProviderWebsite string            `json:"provider_website"`
	Family          string            `json:"family"`
	Modality        string            `json:"modality"`
	Status          string            `json:"status"`
	License         string            `json:"license"`
	OpenWeights     bool              `json:"open_weights"`
	ReleaseDate     *time.Time        `json:"release_date,omitempty"`
	ContextTokens   int64             `json:"context_tokens"`
	MaxOutputTokens int64             `json:"max_output_tokens"`
	SourceURL       string            `json:"source_url"`
	Offerings       []OfferingPrice   `json:"offerings"`
	Results         []BenchmarkResult `json:"benchmark_results"`
	// Board is the model's leaderboard row from the current snapshot, if any.
	Board   *Row     `json:"board,omitempty"`
	Sources []string `json:"sources"`
}

type OfferingPrice struct {
	Offering       string     `json:"offering"`
	Provider       string     `json:"provider"`
	ProviderName   string     `json:"provider_name"`
	Tier           string     `json:"tier"`
	InputMicro     *int64     `json:"input_micro_usd"`
	OutputMicro    *int64     `json:"output_micro_usd"`
	CacheReadMicro *int64     `json:"cache_read_micro_usd"`
	EffectiveAt    *time.Time `json:"effective_at,omitempty"`
	SourceURL      string     `json:"source_url"`
}

type BenchmarkResult struct {
	Benchmark      string    `json:"benchmark"`
	Name           string    `json:"name"`
	Dimension      string    `json:"dimension"`
	Unit           string    `json:"unit"`
	HigherIsBetter bool      `json:"higher_is_better"`
	Value          int64     `json:"value"`
	Settings       string    `json:"settings"`
	SelfRun        bool      `json:"self_run"`
	PublishedAt    time.Time `json:"published_at"`
	SourceURL      string    `json:"source_url"`
}

type BenchmarkInfo struct {
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Dimension      string `json:"dimension"`
	Unit           string `json:"unit"`
	HigherIsBetter bool   `json:"higher_is_better"`
	SourceURL      string `json:"source_url"`
}

// Store is the read surface the API needs.
type Store interface {
	LatestSnapshot(ctx context.Context, method string) (Snapshot, error)
	ModelDetail(ctx context.Context, slug string, now time.Time) (ModelDetail, error)
	Benchmarks(ctx context.Context) ([]BenchmarkInfo, error)
}

// Reader caches the latest snapshot in memory for a short TTL so a traffic
// spike reads MySQL at most once per TTL. On a refresh error it keeps serving
// the previous snapshot rather than failing the page.
type Reader struct {
	store  Store
	method score.Method
	ttl    time.Duration
	now    func() time.Time

	mu      sync.Mutex
	cached  Snapshot
	fetched time.Time
	ok      bool
}

func NewReader(store Store, method score.Method, ttl time.Duration) *Reader {
	return &Reader{store: store, method: method, ttl: ttl, now: time.Now}
}

func (r *Reader) Now() time.Time { return r.now() }

func (r *Reader) Snapshot(ctx context.Context) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ok && r.now().Sub(r.fetched) < r.ttl {
		return r.cached, nil
	}
	snap, err := r.store.LatestSnapshot(ctx, r.method.Version)
	// A board with no models (nothing imported yet) is "not ready", so the
	// page says data is being prepared instead of rendering an empty table.
	if err == nil && len(snap.Rows) == 0 {
		err = ErrNoSnapshot
	}
	if err != nil {
		if r.ok && !errors.Is(err, ErrNoSnapshot) {
			return r.cached, nil
		}
		return Snapshot{}, err
	}
	r.cached, r.fetched, r.ok = snap, r.now(), true
	return snap, nil
}

type Home struct {
	ComputedAt    time.Time `json:"computed_at"`
	MethodVersion string    `json:"method_version"`
	Sources       []string  `json:"sources"`
	ModelCount    int       `json:"model_count"`
	RankedCount   int       `json:"ranked_count"`
	Leaders       []Leader  `json:"leaders"`
	Preview       Page      `json:"preview"`
	Recent        []Row     `json:"recent"`
}

func (r *Reader) Home(ctx context.Context) (Home, error) {
	snap, err := r.Snapshot(ctx)
	if err != nil {
		return Home{}, err
	}
	q := Query{}
	_ = q.Normalize()
	ranked := 0
	for _, row := range snap.Rows {
		if row.Rank != nil {
			ranked++
		}
	}
	now := r.now()
	return Home{
		ComputedAt: snap.ComputedAt, MethodVersion: snap.MethodVersion, Sources: nonNil(snap.Sources),
		ModelCount: len(snap.Rows), RankedCount: ranked,
		Leaders: Leaders(snap), Preview: Filter(snap, q, now), Recent: Recent(snap, now, 6),
	}, nil
}

type Board struct {
	ComputedAt    time.Time `json:"computed_at"`
	MethodVersion string    `json:"method_version"`
	Sources       []string  `json:"sources"`
	Query         Query     `json:"query"`
	Page
}

func (r *Reader) Leaderboard(ctx context.Context, q Query) (Board, error) {
	if err := q.Normalize(); err != nil {
		return Board{}, err
	}
	snap, err := r.Snapshot(ctx)
	if err != nil {
		return Board{}, err
	}
	return Board{ComputedAt: snap.ComputedAt, MethodVersion: snap.MethodVersion, Sources: nonNil(snap.Sources), Query: q, Page: Filter(snap, q, r.now())}, nil
}

func (r *Reader) Model(ctx context.Context, slug string) (ModelDetail, error) {
	d, err := r.store.ModelDetail(ctx, slug, r.now())
	if err != nil {
		return d, err
	}
	// A missing snapshot only hides the rank; the model page still renders.
	d.Sources = []string{}
	if snap, err := r.Snapshot(ctx); err == nil {
		d.Sources = nonNil(snap.Sources)
		for _, row := range snap.Rows {
			if row.Model == slug {
				row := row
				d.Board = &row
				break
			}
		}
	}
	return d, nil
}

type Methodology struct {
	Version        string          `json:"version"`
	Weights        []MethodWeight  `json:"weights"`
	MinCoverageBps int64           `json:"min_coverage_bps"`
	Benchmarks     []BenchmarkInfo `json:"benchmarks"`
	PriceBlend     map[string]int  `json:"price_blend"`
	SpeedWindowH   int             `json:"speed_window_hours"`
	Sources        []string        `json:"sources"`
}

type MethodWeight struct {
	Dimension string `json:"dimension"`
	Weight    int64  `json:"weight"`
}

func (r *Reader) Methodology(ctx context.Context, speedWindow time.Duration) (Methodology, error) {
	benches, err := r.store.Benchmarks(ctx)
	if err != nil {
		return Methodology{}, err
	}
	m := Methodology{
		Version: r.method.Version, MinCoverageBps: r.method.MinCoverageBps, Benchmarks: benches,
		PriceBlend: map[string]int{"input": 3, "output": 1}, SpeedWindowH: int(speedWindow / time.Hour), Sources: []string{},
	}
	if snap, err := r.Snapshot(ctx); err == nil {
		m.Sources = nonNil(snap.Sources)
	}
	for _, w := range r.method.Weights {
		m.Weights = append(m.Weights, MethodWeight{string(w.Dimension), w.Weight})
	}
	return m, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
