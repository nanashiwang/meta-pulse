// Command metar-intel-tool runs migrations, imports catalog/observation
// documents and recomputes the leaderboard on demand.
//
//	metar-intel-tool migrate up|status
//	metar-intel-tool import [--dry-run] <file.json>
//	metar-intel-tool score
//	metar-intel-tool sync-llmstats [--dry-run]
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/config"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/source/llmstats"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
	"github.com/nanashiwang/meta-pulse/services/intel/migrations"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: metar-intel-tool migrate up|status | import [--dry-run] <file.json> | score | sync-llmstats [--dry-run]")
	}
	// A dry run validates the file shape without a database, so documents can
	// be checked locally or in CI.
	if args[0] == "import" && len(args) == 3 && args[1] == "--dry-run" {
		doc, err := readDocument(args[2])
		if err != nil {
			return err
		}
		b, err := catalog.Validate(doc, catalog.Known{})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "valid: %d providers, %d models, %d benchmarks, %d offerings, %d prices, %d results, %d speed samples\n",
			len(b.Providers), len(b.Models), len(b.Benchmarks), len(b.Offerings), len(b.Prices), len(b.Results), len(b.Speeds))
		return nil
	}

	// The llm-stats dry run fetches and validates without a database, to check
	// a new API key and the category mapping before enabling the sync.
	if args[0] == "sync-llmstats" && len(args) == 2 && args[1] == "--dry-run" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		base := os.Getenv("INTEL_LLMSTATS_BASE_URL")
		res, err := llmstats.Sync(ctx, llmstats.NewClient(base, os.Getenv("INTEL_LLMSTATS_API_KEY")), nil, time.Now(), true)
		fmt.Fprintf(out, "requests: %d\n%s\n", res.Requests, res.Report)
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := mysqlstore.Open(cfg.DBDSN)
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	switch {
	case args[0] == "migrate" && len(args) == 2 && args[1] == "up":
		return migrations.Up(ctx, store.SQL())
	case args[0] == "migrate" && len(args) == 2 && args[1] == "status":
		return migrations.Status(ctx, store.SQL())
	case args[0] == "import" && len(args) == 2:
		doc, err := readDocument(args[1])
		if err != nil {
			return err
		}
		known, err := store.Known(ctx)
		if err != nil {
			return err
		}
		b, err := catalog.Validate(doc, known)
		if err != nil {
			return err
		}
		st, err := store.Import(ctx, b)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "imported from %s: %d catalog rows, %d observations added, %d already present\n", b.Source.Slug, st.CatalogRows, st.Added, st.Skipped)
		return score1(ctx, out, store, cfg)
	case args[0] == "score" && len(args) == 1:
		return score1(ctx, out, store, cfg)
	case args[0] == "sync-llmstats" && len(args) == 1:
		res, err := llmstats.Sync(ctx, llmstats.NewClient(cfg.LLMStatsBaseURL, cfg.LLMStatsAPIKey), store, time.Now(), false)
		fmt.Fprintf(out, "requests: %d\n%s\nadded %d, already present %d\n", res.Requests, res.Report, res.Stats.Added, res.Stats.Skipped)
		if err != nil {
			return err
		}
		return score1(ctx, out, store, cfg)
	}
	return fmt.Errorf("unknown command %q", args)
}

func score1(ctx context.Context, out io.Writer, store *mysqlstore.Store, cfg config.Config) error {
	snap, stored, err := service.Recompute(ctx, store, score.Source, time.Now(), cfg.SpeedWindow)
	if err != nil {
		return err
	}
	state := "unchanged"
	if stored {
		state = "stored"
	}
	fmt.Fprintf(out, "leaderboard %s: %d rows, method %s\n", state, len(snap.Rows), snap.MethodVersion)
	return nil
}

// readDocument reads a bounded file and decodes it strictly.
func readDocument(path string) (catalog.Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return catalog.Document{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, catalog.MaxDocumentBytes+1))
	if err != nil {
		return catalog.Document{}, err
	}
	return catalog.DecodeDocument(raw)
}
