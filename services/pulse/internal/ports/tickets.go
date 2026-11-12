package ports

import (
	"context"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"time"
)

// Lots and allocations are projections of the append-only mint/spend ledger.
type TicketLot struct {
	ID             uint64
	UserID         uint64
	PeriodID       uint64
	MintEntryID    uint64
	Issued         int64
	Remaining      int64
	EarnedAt       time.Time
	QuotaExpiresAt time.Time
}

type TicketRepository interface {
	Statistics(context.Context, []uint64) (map[uint64]TicketStatistics, error)
	LockUser(context.Context, uint64) error
	PendingContribution(context.Context, uint64) (int64, error)
	Mint(context.Context, TicketLot) error
	PreviewLots(context.Context, uint64, time.Time) ([]TicketLot, error)
	LotForUpdate(context.Context, uint64) (*TicketLot, error)
	Spend(context.Context, TicketLot, uint64) error
	Period(context.Context, uint64) (period.Period, error)
}

// TicketStatistics is a read-only projection of ticket ledger facts.
type TicketStatistics struct {
	Remaining        int64 `json:"remaining,string"`
	Total            int64 `json:"total,string"`
	QuotaPerUnit     int64 `json:"quota_per_unit,string"`
	Spent            int64 `json:"spent,string"`
	Removed          int64 `json:"removed,string"`
	RewardQuota      int64 `json:"reward_quota,string"`
	RewardExperience int64 `json:"reward_experience,string"`
}
