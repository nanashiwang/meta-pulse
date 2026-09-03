package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const allDimensionsHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type MetricsAggregationConfig struct {
	CursorName   string
	SourceSystem string
	Now          func() time.Time
}

type MetricsAggregationService struct {
	unit ports.UnitOfWork
	cfg  MetricsAggregationConfig
}

func NewMetricsAggregationService(unit ports.UnitOfWork, cfg MetricsAggregationConfig) (*MetricsAggregationService, error) {
	if unit == nil {
		return nil, errors.New("metrics unit of work is nil")
	}
	if cfg.CursorName == "" {
		cfg.CursorName = DefaultUsageCursorName
	}
	if cfg.SourceSystem == "" {
		cfg.SourceSystem = "new-api-log"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &MetricsAggregationService{unit: unit, cfg: cfg}, nil
}

func (s *MetricsAggregationService) Aggregate(ctx context.Context) (ports.OperationalSnapshot, error) {
	var snapshot ports.OperationalSnapshot
	now := s.cfg.Now()
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Operations == nil || repos.Metric == nil {
			return errors.New("operational metric repositories are not initialized")
		}
		var err error
		snapshot, err = repos.Operations.Snapshot(ctx, now, s.cfg.CursorName, s.cfg.SourceSystem)
		if err != nil {
			return err
		}
		values := []struct {
			name  string
			value int64
		}{
			{"ingest_lag_seconds", snapshot.IngestLagSeconds},
			{"ingest_conflict_open", snapshot.OpenConflictCount},
			{"ledger_mismatch", snapshot.LedgerMismatchCount},
			{"settlement_retry", snapshot.SettlementRetryCount},
			{"settlement_dead", snapshot.SettlementDeadCount},
			{"budget_reserved_amount", snapshot.BudgetReservedAmount},
			{"budget_hard_cap", snapshot.BudgetHardCap},
		}
		for _, value := range values {
			if err := repos.Metric.Upsert(ctx, ports.MetricValue{
				MetricDate: now, MetricName: value.name, DimensionHash: allDimensionsHash, Value: value.value,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ports.OperationalSnapshot{}, err
	}
	return snapshot, nil
}

// MetricDimensionHash canonicalizes dimensions so callers never construct a
// mutable identity using a map's iteration order.
func MetricDimensionHash(dimensions any) (string, []byte, error) {
	payload, err := json.Marshal(dimensions)
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), payload, nil
}
