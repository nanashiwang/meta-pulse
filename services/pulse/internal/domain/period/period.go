// Package period defines the time window and lifecycle rules for activities.
package period

import (
	"errors"
	"time"
)

type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusSettling Status = "settling"
	StatusClosed   Status = "closed"
)

var ErrNoActivePeriod = errors.New("no active period for event time")

type Period struct {
	ID            uint64
	Key           string
	Status        Status
	StartsAt      time.Time
	EndsAt        time.Time
	Timezone      string
	ConfigVersion string
	RandomVersion string
}

// Contains uses a half-open interval [starts_at, ends_at). A boundary event
// belongs to the next period, never both periods.
func (p Period) Contains(at time.Time) bool {
	return p.Status == StatusActive && !at.Before(p.StartsAt) && at.Before(p.EndsAt)
}

func ResolveActive(periods []Period, at time.Time) (Period, error) {
	var match Period
	for _, candidate := range periods {
		if !candidate.Contains(at) {
			continue
		}
		if match.ID != 0 {
			return Period{}, errors.New("overlapping active periods")
		}
		match = candidate
	}
	if match.ID == 0 {
		return Period{}, ErrNoActivePeriod
	}
	return match, nil
}
