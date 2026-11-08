package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
)

// ClaimNonce inserts the nonce once; a duplicate means a replayed request.
// Expired rows are pruned opportunistically on each claim.
func (s *Store) ClaimNonce(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM intel_admin_nonce WHERE expires_at < ? LIMIT 500`, time.Now().UTC()); err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO intel_admin_nonce (nonce, expires_at) VALUES (?, ?)`, nonce, expiresAt.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

type Counts struct {
	Providers  int `json:"providers"`
	Models     int `json:"models"`
	Benchmarks int `json:"benchmarks"`
	Offerings  int `json:"offerings"`
	Prices     int `json:"prices"`
	Results    int `json:"benchmark_results"`
	Speeds     int `json:"speed_samples"`
	Snapshots  int `json:"snapshots"`
}

type ImportRun struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	Note        string    `json:"note"`
	ActorID     string    `json:"actor_id"`
	CatalogRows int       `json:"catalog_rows"`
	Added       int       `json:"observations_added"`
	Skipped     int       `json:"observations_skipped"`
	CreatedAt   time.Time `json:"created_at"`
}

type SnapshotMeta struct {
	MethodVersion string    `json:"method_version"`
	Fingerprint   string    `json:"fingerprint"`
	Rows          int       `json:"rows"`
	ComputedAt    time.Time `json:"computed_at"`
}

type Overview struct {
	Counts   Counts        `json:"counts"`
	Snapshot *SnapshotMeta `json:"snapshot"`
	Imports  []ImportRun   `json:"imports"`
}

func (s *Store) Overview(ctx context.Context, method string) (Overview, error) {
	var o Overview
	targets := []struct {
		table string
		dest  *int
	}{
		{"intel_provider", &o.Counts.Providers}, {"intel_model", &o.Counts.Models}, {"intel_benchmark", &o.Counts.Benchmarks},
		{"intel_offering", &o.Counts.Offerings}, {"intel_price_snapshot", &o.Counts.Prices}, {"intel_benchmark_result", &o.Counts.Results},
		{"intel_speed_sample", &o.Counts.Speeds}, {"intel_score_batch", &o.Counts.Snapshots},
	}
	for _, t := range targets {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t.table).Scan(t.dest); err != nil {
			return o, err
		}
	}
	var m SnapshotMeta
	err := s.db.QueryRowContext(ctx, `SELECT method_version, fingerprint, row_count, computed_at FROM intel_score_batch WHERE method_version = ? ORDER BY id DESC LIMIT 1`, method).
		Scan(&m.MethodVersion, &m.Fingerprint, &m.Rows, &m.ComputedAt)
	switch {
	case err == nil:
		m.ComputedAt = m.ComputedAt.UTC()
		o.Snapshot = &m
	case err != sql.ErrNoRows:
		return o, err
	}
	o.Imports = []ImportRun{}
	err = s.each(ctx, `SELECT id, source_slug, note, COALESCE(actor_id, ''), catalog_rows, observations_added, observations_skipped, created_at
		FROM intel_import_run ORDER BY id DESC LIMIT 20`, nil, func(r *sql.Rows) error {
		var run ImportRun
		if err := r.Scan(&run.ID, &run.Source, &run.Note, &run.ActorID, &run.CatalogRows, &run.Added, &run.Skipped, &run.CreatedAt); err != nil {
			return err
		}
		run.CreatedAt = run.CreatedAt.UTC()
		o.Imports = append(o.Imports, run)
		return nil
	})
	return o, err
}

// Export writes the catalog plus each offering's and model's latest
// observations as an import document. Editing it and importing it again
// upserts the catalog and appends only changed observations.
func (s *Store) Export(ctx context.Context, now time.Time) (catalog.Document, error) {
	doc := catalog.Document{
		Source:     catalog.Source{Slug: "admin-export", Note: "Exported " + now.UTC().Format(time.RFC3339) + ". Change source.slug before re-importing."},
		Providers:  []catalog.ProviderDoc{},
		Models:     []catalog.ModelDoc{},
		Benchmarks: []catalog.BenchmarkDoc{},
		Offerings:  []catalog.OfferingDoc{},
		Prices:     []catalog.PriceDoc{},
		Results:    []catalog.ResultDoc{},
		Speeds:     []catalog.SpeedDoc{},
	}
	steps := []struct {
		q    string
		args []any
		fn   func(*sql.Rows) error
	}{
		{`SELECT slug, name, website FROM intel_provider ORDER BY slug`, nil, func(r *sql.Rows) error {
			var p catalog.ProviderDoc
			if err := r.Scan(&p.Slug, &p.Name, &p.Website); err != nil {
				return err
			}
			doc.Providers = append(doc.Providers, p)
			return nil
		}},
		{`SELECT slug, name, provider_slug, family, modality, release_date, status, context_tokens, max_output_tokens, license, open_weights, source_url FROM intel_model ORDER BY slug`, nil, func(r *sql.Rows) error {
			var m catalog.ModelDoc
			var release sql.NullTime
			if err := r.Scan(&m.Slug, &m.Name, &m.Provider, &m.Family, &m.Modality, &release, &m.Status, &m.ContextTokens, &m.MaxOutputTokens, &m.License, &m.OpenWeights, &m.SourceURL); err != nil {
				return err
			}
			if release.Valid {
				m.ReleaseDate = release.Time.UTC().Format("2006-01-02")
			}
			doc.Models = append(doc.Models, m)
			return nil
		}},
		{`SELECT slug, name, dimension, unit, higher_is_better, source_url FROM intel_benchmark ORDER BY slug`, nil, func(r *sql.Rows) error {
			var b catalog.BenchmarkDoc
			var hib bool
			if err := r.Scan(&b.Slug, &b.Name, &b.Dimension, &b.Unit, &hib, &b.SourceURL); err != nil {
				return err
			}
			b.HigherIsBetter = &hib
			doc.Benchmarks = append(doc.Benchmarks, b)
			return nil
		}},
		{`SELECT slug, provider_slug, model_slug, tier FROM intel_offering ORDER BY slug`, nil, func(r *sql.Rows) error {
			var o catalog.OfferingDoc
			if err := r.Scan(&o.Slug, &o.Provider, &o.Model, &o.Tier); err != nil {
				return err
			}
			doc.Offerings = append(doc.Offerings, o)
			return nil
		}},
		{`SELECT offering_slug, input_micro_usd, output_micro_usd, cache_read_micro_usd, effective_at, source_url FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY offering_slug ORDER BY effective_at DESC, id DESC) AS rn FROM intel_price_snapshot WHERE effective_at <= ?) p
			WHERE rn = 1 ORDER BY offering_slug`, []any{now}, func(r *sql.Rows) error {
			var p catalog.PriceDoc
			var in, out int64
			var cache sql.NullInt64
			var eff time.Time
			if err := r.Scan(&p.Offering, &in, &out, &cache, &eff, &p.SourceURL); err != nil {
				return err
			}
			p.InputUSD, p.OutputUSD = FormatMicro(in), FormatMicro(out)
			if cache.Valid {
				p.CacheReadUSD = FormatMicro(cache.Int64)
			}
			p.EffectiveAt = eff.UTC().Format(time.RFC3339)
			doc.Prices = append(doc.Prices, p)
			return nil
		}},
		{`SELECT x.model_slug, x.benchmark_slug, b.unit, x.value, x.settings, x.self_run, x.published_at, x.source_url FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY model_slug, benchmark_slug ORDER BY published_at DESC, id DESC) AS rn FROM intel_benchmark_result WHERE published_at <= ?) x
			JOIN intel_benchmark b ON b.slug = x.benchmark_slug WHERE x.rn = 1 ORDER BY x.model_slug, x.benchmark_slug`, []any{now}, func(r *sql.Rows) error {
			var res catalog.ResultDoc
			var unit string
			var v int64
			var pub time.Time
			if err := r.Scan(&res.Model, &res.Benchmark, &unit, &v, &res.Settings, &res.SelfRun, &pub, &res.SourceURL); err != nil {
				return err
			}
			if unit == "percent" {
				res.Value = formatScaled(v, 2)
			} else {
				res.Value = formatScaled(v, 3)
			}
			res.PublishedAt = pub.UTC().Format(time.RFC3339)
			doc.Results = append(doc.Results, res)
			return nil
		}},
	}
	for _, st := range steps {
		if err := s.each(ctx, st.q, st.args, st.fn); err != nil {
			return doc, err
		}
	}
	return doc, nil
}

// FormatMicro renders micro-USD as a plain decimal string that ParseScaled
// reads back to the same integer.
func FormatMicro(v int64) string { return formatScaled(v, 6) }

func formatScaled(v int64, scale int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	pow := int64(1)
	for i := 0; i < scale; i++ {
		pow *= 10
	}
	whole, frac := v/pow, v%pow
	out := itoa(whole)
	if frac != 0 {
		f := itoa(frac)
		for len(f) < scale {
			f = "0" + f
		}
		for len(f) > 0 && f[len(f)-1] == '0' {
			f = f[:len(f)-1]
		}
		out += "." + f
	}
	if neg {
		return "-" + out
	}
	return out
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// LatestPriceKeys returns the newest effective price per offering, so a sync
// can skip recording prices that did not change.
func (s *Store) LatestPriceKeys(ctx context.Context, now time.Time) (map[string][2]int64, error) {
	out := map[string][2]int64{}
	err := s.each(ctx, `SELECT offering_slug, input_micro_usd, output_micro_usd FROM (
			SELECT offering_slug, input_micro_usd, output_micro_usd,
			  ROW_NUMBER() OVER (PARTITION BY offering_slug ORDER BY effective_at DESC, id DESC) AS rn
			FROM intel_price_snapshot WHERE effective_at <= ?) p WHERE rn = 1`, []any{now}, func(r *sql.Rows) error {
		var slug string
		var in, outPrice int64
		if err := r.Scan(&slug, &in, &outPrice); err != nil {
			return err
		}
		out[slug] = [2]int64{in, outPrice}
		return nil
	})
	return out, err
}

// LastImportAt reports when a source last completed an import.
func (s *Store) LastImportAt(ctx context.Context, source string) (time.Time, bool, error) {
	var t sql.NullTime
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(created_at) FROM intel_import_run WHERE source_slug = ?`, source).Scan(&t); err != nil {
		return time.Time{}, false, err
	}
	return t.Time.UTC(), t.Valid, nil
}
