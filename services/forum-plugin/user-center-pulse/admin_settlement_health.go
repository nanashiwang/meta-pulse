package pulse_user_center

import (
	"errors"
	"time"
)

// Expose counts only; the operations payload also contains private rule details.
type adminSettlementHealth struct {
	ObservedAt   time.Time `json:"observed_at"`
	BlockedCount int64     `json:"blocked_count"`
	RetryCount   int64     `json:"retry_count"`
}

func adminSettlementHealthProjection(data []byte) (adminSettlementHealth, error) {
	var raw struct {
		ObservedAt time.Time `json:"observed_at"`
		Snapshot   *struct {
			Dead  *int64 `json:"settlement_dead_count"`
			Retry *int64 `json:"settlement_retry_count"`
		} `json:"snapshot"`
	}
	if decodeCommunityResponse(data, &raw) != nil || raw.ObservedAt.IsZero() || raw.Snapshot == nil || raw.Snapshot.Dead == nil || raw.Snapshot.Retry == nil || *raw.Snapshot.Dead < 0 || *raw.Snapshot.Retry < 0 || *raw.Snapshot.Dead > (1<<53)-1 || *raw.Snapshot.Retry > (1<<53)-1 {
		return adminSettlementHealth{}, errors.New("settlement health unavailable")
	}
	return adminSettlementHealth{ObservedAt: raw.ObservedAt, BlockedCount: *raw.Snapshot.Dead, RetryCount: *raw.Snapshot.Retry}, nil
}
