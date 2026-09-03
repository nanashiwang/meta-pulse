package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/experiment"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

type memoryExperimentStore struct {
	assignments map[string]ports.ExperimentAssignment
}

func (m *memoryExperimentStore) FindOrCreate(_ context.Context, assignment ports.ExperimentAssignment) (ports.ExperimentAssignment, error) {
	if m.assignments == nil {
		m.assignments = make(map[string]ports.ExperimentAssignment)
	}
	key := assignment.ExperimentID + ":" + strconv.FormatUint(assignment.UserID, 10)
	if existing, ok := m.assignments[key]; ok {
		return existing, nil
	}
	assignment.ID = uint64(len(m.assignments) + 1)
	m.assignments[key] = assignment
	return assignment, nil
}

type experimentUnit struct{ store *memoryExperimentStore }

func (u experimentUnit) Do(ctx context.Context, fn func(ports.Repositories) error) error {
	return fn(ports.Repositories{Experiment: u.store})
}

func TestExperimentAssignmentPersistsFirstCohort(t *testing.T) {
	store := &memoryExperimentStore{}
	now := time.Unix(1700000000, 0).UTC()
	s, err := NewExperimentService(experimentUnit{store}, []byte("experiment-secret"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	variants := []experiment.Variant{{Name: "control", Percentage: 5000}, {Name: "treatment", Percentage: 5000}}
	first, err := s.Assign(context.Background(), "holdout-v1", 7, variants)
	if err != nil {
		t.Fatal(err)
	}
	// A changed candidate allocation must not overwrite the persisted cohort.
	second, err := s.Assign(context.Background(), "holdout-v1", 7, []experiment.Variant{{Name: "new-treatment", Percentage: 10000}})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.AssignedAt != now || len(store.assignments) != 1 {
		t.Fatalf("first=%+v second=%+v assignments=%d", first, second, len(store.assignments))
	}
}
