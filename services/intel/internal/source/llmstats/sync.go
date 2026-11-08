package llmstats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

type Store interface {
	Known(ctx context.Context) (catalog.Known, error)
	LatestPriceKeys(ctx context.Context, now time.Time) (map[string][2]int64, error)
	ImportAs(ctx context.Context, b catalog.Batch, meta mysqlstore.ImportMeta) (mysqlstore.ImportStats, error)
}

type Result struct {
	Report   Report
	Stats    mysqlstore.ImportStats
	Requests int
}

// Sync fetches the full dataset and imports it in one transaction. A
// document that fails validation is rejected whole: partial external data
// never reaches the board.
func Sync(ctx context.Context, c *Client, store Store, now time.Time, dryRun bool) (Result, error) {
	ds, err := c.Fetch(ctx)
	res := Result{Requests: c.Requests}
	if err != nil {
		return res, err
	}
	latest := map[string]PriceKey{}
	known := catalog.Known{}
	if store != nil {
		prices, err := store.LatestPriceKeys(ctx, now)
		if err != nil {
			return res, err
		}
		for k, v := range prices {
			latest[k] = PriceKey{InputMicro: v[0], OutputMicro: v[1]}
		}
		if known, err = store.Known(ctx); err != nil {
			return res, err
		}
	}
	doc, rep := Convert(ds, now, latest)
	res.Report = rep
	b, err := catalog.Validate(doc, known)
	if err != nil {
		var invalid *catalog.ValidationError
		if errors.As(err, &invalid) {
			first := invalid.Problems
			if len(first) > 5 {
				first = first[:5]
			}
			return res, fmt.Errorf("llm-stats data failed validation (%d problems), e.g. %v", len(invalid.Problems), first)
		}
		return res, err
	}
	if dryRun || store == nil {
		return res, nil
	}
	res.Stats, err = store.ImportAs(ctx, b, mysqlstore.ImportMeta{})
	return res, err
}
