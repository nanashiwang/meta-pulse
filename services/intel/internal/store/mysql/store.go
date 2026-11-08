// Package mysql is Intel's only storage. It owns the metar_intel database and
// never connects to Pulse, Answer or new-api databases.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
)

type Store struct{ db *sql.DB }

// Open validates the DSN and forces UTC time handling so DATETIME columns
// round-trip identically on every host.
func Open(dsn string) (*Store, error) {
	if dsn == "" {
		return nil, errors.New("intel database DSN is empty")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse intel database DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Params["charset"] = "utf8mb4"
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Store{db: db}, nil
}

func (s *Store) SQL() *sql.DB                   { return s.db }
func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Known returns existing catalog slugs so an import may reference them.
func (s *Store) Known(ctx context.Context) (catalog.Known, error) {
	k := catalog.Known{Providers: map[string]bool{}, Models: map[string]bool{}, Benchmarks: map[string]bool{}, Offerings: map[string]bool{}, BenchmarkUnits: map[string]string{}}
	for table, set := range map[string]map[string]bool{"intel_provider": k.Providers, "intel_model": k.Models, "intel_offering": k.Offerings} {
		rows, err := s.db.QueryContext(ctx, "SELECT slug FROM "+table)
		if err != nil {
			return k, err
		}
		for rows.Next() {
			var slug string
			if err := rows.Scan(&slug); err != nil {
				rows.Close()
				return k, err
			}
			set[slug] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return k, err
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT slug, unit FROM intel_benchmark")
	if err != nil {
		return k, err
	}
	defer rows.Close()
	for rows.Next() {
		var slug, unit string
		if err := rows.Scan(&slug, &unit); err != nil {
			return k, err
		}
		k.Benchmarks[slug] = true
		k.BenchmarkUnits[slug] = unit
	}
	return k, rows.Err()
}

type ImportStats struct {
	RunID       int64 `json:"run_id"`
	CatalogRows int   `json:"catalog_rows"`
	Added       int   `json:"observations_added"`
	Skipped     int   `json:"observations_skipped"`
	// Replayed is true when an earlier import with the same idempotency key
	// already wrote this request; nothing was written again.
	Replayed bool `json:"replayed"`
}

// ImportMeta identifies an admin import. CLI imports leave it empty.
type ImportMeta struct {
	ActorID, IdempotencyKey, RequestFingerprint string
}

var ErrIdempotencyConflict = errors.New("idempotency key was used for a different request")

// Import writes a validated batch in one transaction. Catalog rows are
// upserted; observations use INSERT IGNORE on their fingerprint so a replay
// adds nothing and is reported as skipped.
func (s *Store) Import(ctx context.Context, b catalog.Batch) (ImportStats, error) {
	return s.ImportAs(ctx, b, ImportMeta{})
}

// ImportAs records who imported and, with an idempotency key, returns the
// original result for a retried request. The unique key on the run row makes
// two concurrent retries resolve to one write.
func (s *Store) ImportAs(ctx context.Context, b catalog.Batch, meta ImportMeta) (ImportStats, error) {
	if meta.IdempotencyKey != "" {
		if st, found, err := s.importByKey(ctx, meta); found || err != nil {
			return st, err
		}
	}
	st, err := s.importTx(ctx, b, meta)
	var dup *mysql.MySQLError
	if meta.IdempotencyKey != "" && errors.As(err, &dup) && dup.Number == 1062 {
		if prior, found, err := s.importByKey(ctx, meta); found || err != nil {
			return prior, err
		}
	}
	return st, err
}

func (s *Store) importByKey(ctx context.Context, meta ImportMeta) (ImportStats, bool, error) {
	var st ImportStats
	var fp sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, catalog_rows, observations_added, observations_skipped, request_fingerprint FROM intel_import_run WHERE idempotency_key = ?`,
		meta.IdempotencyKey).Scan(&st.RunID, &st.CatalogRows, &st.Added, &st.Skipped, &fp)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	if fp.String != meta.RequestFingerprint {
		return ImportStats{}, true, ErrIdempotencyConflict
	}
	st.Replayed = true
	return st, true, nil
}

func (s *Store) importTx(ctx context.Context, b catalog.Batch, meta ImportMeta) (ImportStats, error) {
	var st ImportStats
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return st, err
	}
	defer tx.Rollback()

	exec := func(q string, args ...any) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}
	for _, p := range b.Providers {
		if err := exec(`INSERT INTO intel_provider (slug, name, website) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE name = VALUES(name), website = VALUES(website)`, p.Slug, p.Name, p.Website); err != nil {
			return st, fmt.Errorf("provider %s: %w", p.Slug, err)
		}
		st.CatalogRows++
	}
	for _, m := range b.Models {
		if err := exec(`INSERT INTO intel_model (slug, name, provider_slug, family, modality, status, license, open_weights, release_date, context_tokens, max_output_tokens, source_url)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE name = VALUES(name), provider_slug = VALUES(provider_slug), family = VALUES(family), modality = VALUES(modality),
			  status = VALUES(status), license = VALUES(license), open_weights = VALUES(open_weights), release_date = VALUES(release_date),
			  context_tokens = VALUES(context_tokens), max_output_tokens = VALUES(max_output_tokens), source_url = VALUES(source_url)`,
			m.Slug, m.Name, m.Provider, m.Family, m.Modality, m.Status, m.License, m.OpenWeights, nullDate(m.ReleaseDate), m.ContextTokens, m.MaxOutputTokens, m.SourceURL); err != nil {
			return st, fmt.Errorf("model %s: %w", m.Slug, err)
		}
		st.CatalogRows++
	}
	for _, x := range b.Benchmarks {
		if err := exec(`INSERT INTO intel_benchmark (slug, name, dimension, unit, higher_is_better, source_url) VALUES (?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE name = VALUES(name), dimension = VALUES(dimension), higher_is_better = VALUES(higher_is_better), source_url = VALUES(source_url)`,
			x.Slug, x.Name, string(x.Dimension), x.Unit, x.HigherIsBetter, x.SourceURL); err != nil {
			return st, fmt.Errorf("benchmark %s: %w", x.Slug, err)
		}
		st.CatalogRows++
	}
	for _, o := range b.Offerings {
		if err := exec(`INSERT INTO intel_offering (slug, provider_slug, model_slug, tier) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE provider_slug = VALUES(provider_slug), model_slug = VALUES(model_slug), tier = VALUES(tier)`,
			o.Slug, o.Provider, o.Model, o.Tier); err != nil {
			return st, fmt.Errorf("offering %s: %w", o.Slug, err)
		}
		st.CatalogRows++
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO intel_import_run (source_slug, note, actor_id, idempotency_key, request_fingerprint, catalog_rows, observations_added, observations_skipped)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0)`,
		b.Source.Slug, b.Source.Note, nullString(meta.ActorID), nullString(meta.IdempotencyKey), nullString(meta.RequestFingerprint), st.CatalogRows)
	if err != nil {
		return st, err
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return st, err
	}
	st.RunID = runID
	count := func(q string, args ...any) error {
		r, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			st.Added++
		} else {
			st.Skipped++
		}
		return nil
	}
	for _, p := range b.Prices {
		if err := count(`INSERT IGNORE INTO intel_price_snapshot (offering_slug, input_micro_usd, output_micro_usd, cache_read_micro_usd, effective_at, source_slug, source_url, fingerprint, import_run_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.Offering, p.InputMicro, p.OutputMicro, p.CacheReadMicro, p.EffectiveAt, b.Source.Slug, p.SourceURL, p.Fingerprint, runID); err != nil {
			return st, fmt.Errorf("price %s: %w", p.Offering, err)
		}
	}
	for _, r := range b.Results {
		if err := count(`INSERT IGNORE INTO intel_benchmark_result (model_slug, benchmark_slug, value, settings, self_run, published_at, source_slug, source_url, fingerprint, import_run_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.Model, r.Benchmark, r.Value, r.Settings, r.SelfRun, r.PublishedAt, b.Source.Slug, r.SourceURL, r.Fingerprint, runID); err != nil {
			return st, fmt.Errorf("result %s/%s: %w", r.Model, r.Benchmark, err)
		}
	}
	for _, sp := range b.Speeds {
		if err := count(`INSERT IGNORE INTO intel_speed_sample (offering_slug, output_tokens_per_sec_milli, ttft_ms, measured_at, source_slug, fingerprint, import_run_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sp.Offering, sp.OutputTokPerSecMilli, sp.TTFTMillis, sp.MeasuredAt, b.Source.Slug, sp.Fingerprint, runID); err != nil {
			return st, fmt.Errorf("speed %s: %w", sp.Offering, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE intel_import_run SET observations_added = ?, observations_skipped = ? WHERE id = ?`, st.Added, st.Skipped, runID); err != nil {
		return st, err
	}
	return st, tx.Commit()
}

// LoadInputs reads the current catalog, the latest effective price per
// offering, the latest result per model×benchmark, and median speeds within
// the window. Future-dated prices are ignored until they take effect.
func (s *Store) LoadInputs(ctx context.Context, now time.Time, speedWindow time.Duration) (service.Inputs, error) {
	in := service.Inputs{LatestPrice: map[string]service.PriceInfo{}, Results: map[string]map[string]int64{}, MedianSpeed: map[string]int64{}}

	if err := s.each(ctx, `SELECT slug, name FROM intel_provider ORDER BY slug`, nil, func(r *sql.Rows) error {
		var p service.ProviderInfo
		if err := r.Scan(&p.Slug, &p.Name); err != nil {
			return err
		}
		in.Providers = append(in.Providers, p)
		return nil
	}); err != nil {
		return in, err
	}
	if err := s.each(ctx, `SELECT slug, name, provider_slug, modality, status, license, open_weights, release_date, context_tokens FROM intel_model ORDER BY slug`, nil, func(r *sql.Rows) error {
		var m service.ModelInfo
		var release sql.NullTime
		if err := r.Scan(&m.Slug, &m.Name, &m.Provider, &m.Modality, &m.Status, &m.License, &m.OpenWeights, &release, &m.ContextTokens); err != nil {
			return err
		}
		if release.Valid {
			t := release.Time.UTC()
			m.ReleaseDate = &t
		}
		in.Models = append(in.Models, m)
		return nil
	}); err != nil {
		return in, err
	}
	if err := s.each(ctx, `SELECT slug, dimension, higher_is_better FROM intel_benchmark ORDER BY slug`, nil, func(r *sql.Rows) error {
		var b score.Benchmark
		var dim string
		if err := r.Scan(&b.Slug, &dim, &b.HigherIsBetter); err != nil {
			return err
		}
		b.Dimension = score.Dimension(dim)
		in.Benchmarks = append(in.Benchmarks, b)
		return nil
	}); err != nil {
		return in, err
	}
	if err := s.each(ctx, `SELECT slug, provider_slug, model_slug, tier FROM intel_offering ORDER BY slug`, nil, func(r *sql.Rows) error {
		var o service.OfferingInfo
		if err := r.Scan(&o.Slug, &o.Provider, &o.Model, &o.Tier); err != nil {
			return err
		}
		in.Offerings = append(in.Offerings, o)
		return nil
	}); err != nil {
		return in, err
	}
	if err := s.each(ctx, `SELECT offering_slug, input_micro_usd, output_micro_usd, effective_at FROM (
			SELECT offering_slug, input_micro_usd, output_micro_usd, effective_at,
			  ROW_NUMBER() OVER (PARTITION BY offering_slug ORDER BY effective_at DESC, id DESC) AS rn
			FROM intel_price_snapshot WHERE effective_at <= ?) p WHERE rn = 1`, []any{now}, func(r *sql.Rows) error {
		var slug string
		var p service.PriceInfo
		if err := r.Scan(&slug, &p.InputMicro, &p.OutputMicro, &p.EffectiveAt); err != nil {
			return err
		}
		in.LatestPrice[slug] = p
		return nil
	}); err != nil {
		return in, err
	}
	if err := s.each(ctx, `SELECT model_slug, benchmark_slug, value FROM (
			SELECT model_slug, benchmark_slug, value,
			  ROW_NUMBER() OVER (PARTITION BY model_slug, benchmark_slug ORDER BY published_at DESC, id DESC) AS rn
			FROM intel_benchmark_result WHERE published_at <= ?) x WHERE rn = 1`, []any{now}, func(r *sql.Rows) error {
		var model, bench string
		var v int64
		if err := r.Scan(&model, &bench, &v); err != nil {
			return err
		}
		if in.Results[model] == nil {
			in.Results[model] = map[string]int64{}
		}
		in.Results[model][bench] = v
		return nil
	}); err != nil {
		return in, err
	}
	in.Sources = []string{}
	if err := s.each(ctx, `SELECT source_slug FROM intel_price_snapshot UNION SELECT source_slug FROM intel_benchmark_result
		UNION SELECT source_slug FROM intel_speed_sample ORDER BY source_slug`, nil, func(r *sql.Rows) error {
		var src string
		if err := r.Scan(&src); err != nil {
			return err
		}
		in.Sources = append(in.Sources, src)
		return nil
	}); err != nil {
		return in, err
	}
	samples := map[string][]int64{}
	if err := s.each(ctx, `SELECT offering_slug, output_tokens_per_sec_milli FROM intel_speed_sample WHERE measured_at > ? AND measured_at <= ?`,
		[]any{now.Add(-speedWindow), now}, func(r *sql.Rows) error {
			var slug string
			var v int64
			if err := r.Scan(&slug, &v); err != nil {
				return err
			}
			samples[slug] = append(samples[slug], v)
			return nil
		}); err != nil {
		return in, err
	}
	for slug, vs := range samples {
		in.MedianSpeed[slug] = Median(vs)
	}
	return in, nil
}

// Median of positive integers; an even count averages the middle pair with
// truncation.
func Median(vs []int64) int64 {
	if len(vs) == 0 {
		return 0
	}
	sorted := append([]int64(nil), vs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return sorted[mid-1] + (sorted[mid]-sorted[mid-1])/2
}

// SaveSnapshot stores a board unless an identical one already exists.
func (s *Store) SaveSnapshot(ctx context.Context, snap service.Snapshot) (bool, error) {
	payload, err := json.Marshal(snap)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO intel_score_batch (method_version, fingerprint, row_count, payload, computed_at) VALUES (?, ?, ?, ?, ?)`,
		snap.MethodVersion, snap.Fingerprint, len(snap.Rows), string(payload), snap.ComputedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (s *Store) LatestSnapshot(ctx context.Context, method string) (service.Snapshot, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM intel_score_batch WHERE method_version = ? ORDER BY id DESC LIMIT 1`, method).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Snapshot{}, service.ErrNoSnapshot
	}
	if err != nil {
		return service.Snapshot{}, err
	}
	var snap service.Snapshot
	if err := json.Unmarshal([]byte(payload), &snap); err != nil {
		return service.Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	return snap, nil
}

// ModelDetail reads live observations for one model page.
func (s *Store) ModelDetail(ctx context.Context, slug string, now time.Time) (service.ModelDetail, error) {
	var d service.ModelDetail
	var release sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT m.slug, m.name, m.provider_slug, p.name, p.website, m.family, m.modality, m.status, m.license, m.open_weights,
			m.release_date, m.context_tokens, m.max_output_tokens, m.source_url
		FROM intel_model m JOIN intel_provider p ON p.slug = m.provider_slug WHERE m.slug = ?`, slug).Scan(
		&d.Slug, &d.Name, &d.Provider, &d.ProviderName, &d.ProviderWebsite, &d.Family, &d.Modality, &d.Status, &d.License, &d.OpenWeights,
		&release, &d.ContextTokens, &d.MaxOutputTokens, &d.SourceURL)
	if errors.Is(err, sql.ErrNoRows) {
		return d, service.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if release.Valid {
		t := release.Time.UTC()
		d.ReleaseDate = &t
	}
	d.Offerings = []service.OfferingPrice{}
	d.Results = []service.BenchmarkResult{}
	if err := s.each(ctx, `SELECT o.slug, o.provider_slug, pr.name, o.tier, p.input_micro_usd, p.output_micro_usd, p.cache_read_micro_usd, p.effective_at, p.source_url
		FROM intel_offering o JOIN intel_provider pr ON pr.slug = o.provider_slug
		LEFT JOIN (
			SELECT offering_slug, input_micro_usd, output_micro_usd, cache_read_micro_usd, effective_at, source_url,
			  ROW_NUMBER() OVER (PARTITION BY offering_slug ORDER BY effective_at DESC, id DESC) AS rn
			FROM intel_price_snapshot WHERE effective_at <= ?) p ON p.offering_slug = o.slug AND p.rn = 1
		WHERE o.model_slug = ? ORDER BY o.slug`, []any{now, slug}, func(r *sql.Rows) error {
		var o service.OfferingPrice
		var in, out, cache sql.NullInt64
		var eff sql.NullTime
		var src sql.NullString
		if err := r.Scan(&o.Offering, &o.Provider, &o.ProviderName, &o.Tier, &in, &out, &cache, &eff, &src); err != nil {
			return err
		}
		o.InputMicro, o.OutputMicro, o.CacheReadMicro = nullInt(in), nullInt(out), nullInt(cache)
		if eff.Valid {
			t := eff.Time.UTC()
			o.EffectiveAt = &t
		}
		o.SourceURL = src.String
		d.Offerings = append(d.Offerings, o)
		return nil
	}); err != nil {
		return d, err
	}
	err = s.each(ctx, `SELECT b.slug, b.name, b.dimension, b.unit, b.higher_is_better, x.value, x.settings, x.self_run, x.published_at, x.source_url FROM (
			SELECT benchmark_slug, value, settings, self_run, published_at, source_url,
			  ROW_NUMBER() OVER (PARTITION BY benchmark_slug ORDER BY published_at DESC, id DESC) AS rn
			FROM intel_benchmark_result WHERE model_slug = ? AND published_at <= ?) x
		JOIN intel_benchmark b ON b.slug = x.benchmark_slug WHERE x.rn = 1 ORDER BY b.dimension, b.slug`, []any{slug, now}, func(r *sql.Rows) error {
		var b service.BenchmarkResult
		if err := r.Scan(&b.Benchmark, &b.Name, &b.Dimension, &b.Unit, &b.HigherIsBetter, &b.Value, &b.Settings, &b.SelfRun, &b.PublishedAt, &b.SourceURL); err != nil {
			return err
		}
		b.PublishedAt = b.PublishedAt.UTC()
		d.Results = append(d.Results, b)
		return nil
	})
	return d, err
}

// Benchmarks lists benchmark definitions for the methodology page.
func (s *Store) Benchmarks(ctx context.Context) ([]service.BenchmarkInfo, error) {
	out := []service.BenchmarkInfo{}
	err := s.each(ctx, `SELECT slug, name, dimension, unit, higher_is_better, source_url FROM intel_benchmark ORDER BY dimension, slug`, nil, func(r *sql.Rows) error {
		var b service.BenchmarkInfo
		if err := r.Scan(&b.Slug, &b.Name, &b.Dimension, &b.Unit, &b.HigherIsBetter, &b.SourceURL); err != nil {
			return err
		}
		out = append(out, b)
		return nil
	})
	return out, err
}

func (s *Store) each(ctx context.Context, q string, args []any, fn func(*sql.Rows) error) error {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}
