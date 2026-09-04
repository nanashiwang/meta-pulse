package ports

import (
	"context"
	"time"
)

// ContentEvent is the normalized, metadata-only input contract for content
// ingestion. Adapters may read a forum schema, but services do not depend on
// a concrete forum implementation.
type ContentEvent struct {
	SourceContentID string
	ContentType     string
	AuthorUserID    uint64
	Title           string
	SourceCreatedAt time.Time
	CursorValue     string
	PayloadHash     string
}

// ContentCandidate is a metadata-only projection from the forum. Pulse never
// copies forum bodies and never writes to the forum database.
type ContentCandidate struct {
	ID              uint64
	SourceSystem    string
	SourceContentID string
	ContentType     string
	AuthorUserID    uint64
	PeriodID        uint64
	Title           string
	SourceCreatedAt time.Time
	PayloadHash     string
	CursorValue     string
	Status          string
	ReviewActorType string
	ReviewActorID   string
	ReviewReason    string
	ReviewedAt      *time.Time
	CreatedAt       time.Time
}

type ContentAward struct {
	ID           uint64
	CandidateID  uint64
	AwardVersion uint64
	ActionID     string
	PeriodID     uint64
	UserID       uint64
	Amount       int64
	RewardType   string
	BudgetType   string
	GrantID      string
	Status       string
	Reason       string
	CreatedAt    time.Time
}

const (
	ContentCandidatePending  = "pending"
	ContentCandidateApproved = "approved"
	ContentCandidateRejected = "rejected"
	ContentCandidateDeleted  = "deleted"

	ContentAwardPending    = "pending"
	ContentAwardSettled    = "settled"
	ContentAwardIneligible = "ineligible"
	ContentAwardLimited    = "limited"
	ContentAwardReversed   = "reversed"
)

type ContentRepository interface {
	FindCandidateBySource(ctx context.Context, sourceSystem, sourceContentID string) (*ContentCandidate, error)
	FindCandidateForUpdate(ctx context.Context, candidateID uint64) (*ContentCandidate, error)
	CreateCandidate(ctx context.Context, candidate ContentCandidate) (ContentCandidate, error)
	ReviewCandidate(ctx context.Context, candidateID uint64, status, actorType, actorID, reason string, reviewedAt time.Time) error
	FindAwardByAction(ctx context.Context, actionID string) (*ContentAward, error)
	CreateAward(ctx context.Context, award ContentAward) (ContentAward, error)
	UpdateAwardStatus(ctx context.Context, actionID, status string) error
	MarkAwardSettledByGrantID(ctx context.Context, grantID string) error
	SumUserActiveAwards(ctx context.Context, userID, periodID uint64) (int64, error)
	SumDailyActiveAwards(ctx context.Context, day time.Time) (int64, error)
}
