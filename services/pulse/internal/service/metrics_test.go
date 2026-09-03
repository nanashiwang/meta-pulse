package service

import (
	"context"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryOperationsStore struct {
	snapshot ports.OperationalSnapshot
}

func (m *memoryOperationsStore) Snapshot(context.Context, time.Time, string, string) (ports.OperationalSnapshot, error) {
	return m.snapshot, nil
}

type memoryMetricStore struct {
	values []ports.MetricValue
}

func (m *memoryMetricStore) Upsert(_ context.Context, value ports.MetricValue) error {
	m.values = append(m.values, value)
	return nil
}

type metricsUnit struct {
	operations *memoryOperationsStore
	metrics    *memoryMetricStore
}

func (u metricsUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Operations: u.operations, Metric: u.metrics})
}

func TestMetricsAggregationPersistsOperationalSnapshot(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	operations := &memoryOperationsStore{snapshot: ports.OperationalSnapshot{
		IngestLagSeconds: 11, OpenConflictCount: 2, LedgerMismatchCount: 1,
		SettlementRetryCount: 3, SettlementDeadCount: 4, BudgetReservedAmount: 500, BudgetHardCap: 1000,
	}}
	metrics := &memoryMetricStore{}
	s, err := NewMetricsAggregationService(metricsUnit{operations, metrics}, MetricsAggregationConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Aggregate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != operations.snapshot || len(metrics.values) != 7 {
		t.Fatalf("snapshot=%+v values=%d", got, len(metrics.values))
	}
	for _, value := range metrics.values {
		if value.MetricDate != now || value.DimensionHash == "" || value.Dimensions != nil {
			t.Fatalf("metric=%+v", value)
		}
	}
}

func TestMetricDimensionHashIsDeterministic(t *testing.T) {
	first, firstJSON, err := MetricDimensionHash(struct {
		Region string `json:"region"`
	}{Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	second, secondJSON, err := MetricDimensionHash(struct {
		Region string `json:"region"`
	}{Region: "cn"})
	if err != nil || first != second || string(firstJSON) != string(secondJSON) {
		t.Fatalf("first=%q second=%q err=%v", first, second, err)
	}
}
