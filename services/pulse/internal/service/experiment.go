package service

import (
	"context"
	"errors"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/experiment"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// ExperimentService derives a stable cohort and persists the first assignment.
// FindOrCreate always returns the historical row when the same experiment/user
// is seen again, so a later configuration change cannot silently regroup users.
type ExperimentService struct {
	unit   ports.UnitOfWork
	secret []byte
	now    func() time.Time
}

func NewExperimentService(unit ports.UnitOfWork, secret []byte, now func() time.Time) (*ExperimentService, error) {
	if unit == nil {
		return nil, errors.New("experiment unit of work is nil")
	}
	if len(secret) == 0 {
		return nil, experiment.ErrInvalidAssignment
	}
	if now == nil {
		now = time.Now
	}
	return &ExperimentService{unit: unit, secret: append([]byte(nil), secret...), now: now}, nil
}

func (s *ExperimentService) Assign(ctx context.Context, experimentID string, userID uint64, variants []experiment.Variant) (ports.ExperimentAssignment, error) {
	assignment, err := experiment.Assign(s.secret, experimentID, userID, variants)
	if err != nil {
		return ports.ExperimentAssignment{}, err
	}
	candidate := ports.ExperimentAssignment{
		ExperimentID: assignment.ExperimentID,
		UserID:       assignment.UserID,
		Cohort:       assignment.Cohort,
		BucketBps:    assignment.BucketBps,
		AssignedAt:   s.now(),
	}
	var persisted ports.ExperimentAssignment
	err = s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Experiment == nil {
			return errors.New("experiment repository is not initialized")
		}
		var err error
		persisted, err = repos.Experiment.FindOrCreate(ctx, candidate)
		return err
	})
	if err != nil {
		return ports.ExperimentAssignment{}, err
	}
	return persisted, nil
}
