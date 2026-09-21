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
	Continuous        bool
	QuotaValidityDays int

	ID                   uint64
	Key                  string
	Status               Status
	StartsAt             time.Time
	EndsAt               time.Time
	Timezone             string
	ConfigVersion        string
	RandomVersion        string
	FundingPolicy        string
	TicketThresholdMilli int64
}

// Contains uses a half-open interval [starts_at, ends_at). A boundary event
// belongs to the next period, never both periods.
func (p Period) Contains(at time.Time) bool {
	return p.Status == StatusActive && !at.Before(p.StartsAt) && at.Before(p.EndsAt)
}

func ResolveActive(periods []Period, at time.Time) (Period, error) {
	// Continuous snapshots supersede earlier snapshots for new usage only.
	var latest Period
	for _, p := range periods {
		if p.Continuous && p.Contains(at) && (latest.ID == 0 || p.StartsAt.After(latest.StartsAt) || p.StartsAt.Equal(latest.StartsAt) && p.ID > latest.ID) {
			latest = p
		}
	}
	if latest.ID != 0 {
		return latest, nil
	}
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

const VerifiedPaidFunding = "verified-paid-v1"
