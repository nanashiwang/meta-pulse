package ports

import (
	"context"
	"time"
)

// ExperienceDelivery owns the community-only outbox states. The monetary
// settlement worker never leases these rows or sends them to new-api.
type ExperienceDelivery interface {
	ListPendingExperience(context.Context, uint64, int) ([]RewardGrant, error)
	ExperienceDeliveryStatusForUpdate(context.Context, uint64) (string, error)
	TransitionExperienceDelivery(context.Context, uint64, string, string, time.Time) error
}
