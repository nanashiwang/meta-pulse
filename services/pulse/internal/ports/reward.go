package ports

import (
	"context"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/reward"
)

type RewardBudget struct {
	ID             uint64
	PeriodID       uint64
	BudgetType     string
	HardCap        int64
	ReservedAmount int64
	SettledAmount  int64
	ReleasedAmount int64
	Version        uint64
}

type RewardGrant struct {
	ID                 uint64
	GrantID            string
	PeriodID           uint64
	UserID             uint64
	ActionID           string
	TriggerType        string
	RewardDefinitionID uint64
	RewardType         string
	Amount             int64
	TransferableQuota  bool
	BudgetType         string
	RandomValue        string
	ConfigVersion      string
	Status             string
	SourceRef          string
	Reason             string
	SettledAt          *time.Time
	ReversedAt         *time.Time
	CreatedAt          time.Time
}

type SettlementOutbox struct {
	ID            uint64
	RewardGrantID uint64
	Operation     string
	PayloadHash   string
	PayloadJSON   []byte
	Status        string
	Attempts      uint32
	NextAttemptAt time.Time
	LeasedUntil   *time.Time
	LastError     string
	CompletedAt   *time.Time
	CreatedAt     time.Time
}

type IdempotencyRecord struct {
	ID             uint64
	Scope          string
	Key            string
	PayloadHash    string
	ResponseStatus *int
	ResponseJSON   []byte
	ResourceType   string
	ResourceID     string
	ExpiresAt      *time.Time
}

// RewardHistoryRepository exposes only the user-safe read model needed by
// the product/API layer. It is separate from mutation-oriented RewardRepository
// so a read endpoint cannot accidentally gain mutation semantics.
type RewardHistoryRepository interface {
	ListGrantsForUser(ctx context.Context, userID uint64, limit int) ([]RewardGrant, error)
}

type RewardRepository interface {
	ListDefinitions(ctx context.Context, periodID uint64) ([]reward.Definition, error)
	GetBudgetForUpdate(ctx context.Context, periodID uint64, budgetType string) (RewardBudget, error)
	SaveBudget(ctx context.Context, budget RewardBudget) error
	FindGrantByAction(ctx context.Context, periodID, userID uint64, actionID string) (*RewardGrant, error)
	FindGrantByID(ctx context.Context, grantID uint64) (*RewardGrant, error)
	FindGrantByPublicID(ctx context.Context, grantID string) (*RewardGrant, error)
	FindGrantByIDForUpdate(ctx context.Context, grantID uint64) (*RewardGrant, error)
	TransitionGrantStatus(ctx context.Context, grantID uint64, fromStatus, toStatus string, at time.Time) error
	CreateGrant(ctx context.Context, grant RewardGrant) (RewardGrant, error)
	CreateOutbox(ctx context.Context, outbox SettlementOutbox) (SettlementOutbox, error)
}

type IdempotencyRepository interface {
	GetOrCreateForUpdate(ctx context.Context, scope, key, payloadHash string) (IdempotencyRecord, error)
	Save(ctx context.Context, record IdempotencyRecord) error
}

type SettlementRepository interface {
	FindByGrant(ctx context.Context, rewardGrantID uint64) (*SettlementOutbox, error)
	ClaimDue(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]SettlementOutbox, error)
	ClaimDead(ctx context.Context, rewardGrantID uint64, expectedAttempts uint32, now time.Time, leaseUntil time.Time) (SettlementOutbox, error)
	ListForReconciliation(ctx context.Context, limit int) ([]SettlementOutbox, error)
	SaveOutbox(ctx context.Context, outbox SettlementOutbox) error
}
