// Command metar-intel-worker syncs external sources and rebuilds the
// leaderboard snapshot. Scheduled syncs run once per source interval; an
// admin "sync now" request is picked up within the short poll interval.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/config"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/domain/score"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/service"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/settings"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/source/llmstats"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

const (
	requestPoll  = 30 * time.Second
	retryBackoff = time.Hour
)

type worker struct {
	logger *slog.Logger
	store  *mysqlstore.Store
	cfg    config.Config
	box    *settings.Box
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	store, err := mysqlstore.Open(cfg.DBDSN)
	if err != nil {
		logger.Error("open intel database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	w := &worker{logger: logger, store: store, cfg: cfg}
	if len(cfg.AdminSecrets) > 0 {
		w.box, _ = settings.NewBox(cfg.AdminSecrets[0])
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("metar-intel-worker started", "interval", cfg.ScoreInterval.String(), "llmstats_env_key", cfg.LLMStatsAPIKey != "", "ui_keys", w.box != nil)
	scoreTicker := time.NewTicker(cfg.ScoreInterval)
	defer scoreTicker.Stop()
	poll := time.NewTicker(requestPoll)
	defer poll.Stop()
	w.syncLLMStats(ctx, w.syncRequested(ctx))
	w.recompute(ctx)
	for {
		select {
		case <-ctx.Done():
			logger.Info("metar-intel-worker stopped")
			return
		case <-scoreTicker.C:
			w.syncLLMStats(ctx, false)
			w.recompute(ctx)
		case <-poll.C:
			if w.syncRequested(ctx) && w.syncLLMStats(ctx, true) {
				w.recompute(ctx)
			}
		}
	}
}

// recompute never exits the process on failure: the previous snapshot keeps
// being served and the next tick retries.
func (w *worker) recompute(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	snap, stored, err := service.Recompute(ctx, w.store, score.Source, time.Now(), w.cfg.SpeedWindow)
	if err != nil {
		w.logger.Error("recompute leaderboard", "error", err)
		return
	}
	w.logger.Info("leaderboard recomputed", "method", snap.MethodVersion, "rows", len(snap.Rows), "stored", stored, "fingerprint", snap.Fingerprint[:12])
}

func (w *worker) syncRequested(ctx context.Context) bool {
	st, ok, err := w.store.SourceState(ctx, llmstats.SourceSlug)
	return err == nil && ok && st.SyncRequestedAt != nil
}

// syncLLMStats returns true when it imported. Scheduled runs happen at most
// once per interval (measured from the last successful import, so restarts
// never spend extra quota) and back off for an hour after a failure; forced
// runs come from an admin request.
func (w *worker) syncLLMStats(ctx context.Context, forced bool) bool {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	key, origin, keyErr := llmstats.EffectiveKey(ctx, w.store, w.box, w.cfg.LLMStatsAPIKey)
	started := time.Now()
	if keyErr != nil {
		msg := "saved API key cannot be decrypted; enter it again in /admin/models"
		if !errors.Is(keyErr, settings.ErrStale) && !errors.Is(keyErr, settings.ErrUnavailable) {
			msg = "read source settings failed"
		}
		if forced {
			_ = w.store.RecordSourceSync(ctx, llmstats.SourceSlug, started, false, "error", msg, "", 0)
		}
		w.logger.Error("llm-stats key unavailable", "error", keyErr)
		return false
	}
	if key == "" {
		if forced {
			_ = w.store.RecordSourceSync(ctx, llmstats.SourceSlug, started, false, "error", "no API key configured", "", 0)
		}
		return false
	}
	if _, err := w.store.LatestSnapshot(ctx, score.Source.Version); errors.Is(err, service.ErrNoSnapshot) {
		forced = true
	}
	if !forced {
		if last, ok, err := w.store.LastImportAt(ctx, llmstats.SourceSlug); err != nil || (ok && time.Since(last) < w.cfg.LLMStatsSyncInterval) {
			return false
		}
		if st, ok, err := w.store.SourceState(ctx, llmstats.SourceSlug); err == nil && ok && st.LastStatus == "error" && st.LastAttemptAt != nil && time.Since(*st.LastAttemptAt) < retryBackoff {
			return false
		}
	}
	res, err := llmstats.Sync(ctx, llmstats.NewClient(w.cfg.LLMStatsBaseURL, key), w.store, started, false)
	if err != nil {
		w.logger.Error("llm-stats sync failed", "error", err, "requests", res.Requests, "origin", origin)
		_ = w.store.RecordSourceSync(ctx, llmstats.SourceSlug, started, false, "error", err.Error(), res.Report.String(), res.Requests)
		return false
	}
	w.logger.Info("llm-stats sync done", "requests", res.Requests, "added", res.Stats.Added, "existing", res.Stats.Skipped, "origin", origin, "report", res.Report.String())
	_ = w.store.RecordSourceSync(ctx, llmstats.SourceSlug, started, true, "ok", "", res.Report.String(), res.Requests)
	return true
}
